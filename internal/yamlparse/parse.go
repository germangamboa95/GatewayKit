package yamlparse

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type srcLine struct {
	num    int
	text   string
	indent int
}

type parser struct {
	lines []srcLine
	i     int
	off   int
}

// ParseYAML parses src into a node tree. Errors are *Error values carrying
// the line and column of the problem.
func ParseYAML(src []byte) (*Node, error) {
	lines, err := scanLines(string(src))
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return &Node{Kind: Null, Line: 1, Col: 1}, nil
	}
	p := &parser{lines: lines, off: lines[0].indent}
	n, err := p.parseNode(-1)
	if err != nil {
		return nil, err
	}
	if !p.eof() {
		ln := p.line()
		return nil, errAt(ln, ln.indent, "unexpected indentation")
	}
	return n, nil
}

func errAt(ln *srcLine, off int, format string, args ...any) *Error {
	return &Error{Line: ln.num, Col: colOf(ln, off), Msg: fmt.Sprintf(format, args...)}
}

func colOf(ln *srcLine, off int) int {
	if off > len(ln.text) {
		off = len(ln.text)
	}
	return utf8.RuneCountInString(ln.text[:off]) + 1
}

func isWS(c byte) bool { return c == ' ' || c == '\t' }

func skipWS(t string, j int) int {
	for j < len(t) && isWS(t[j]) {
		j++
	}
	return j
}

func isMarker(t, m string) bool {
	return strings.HasPrefix(t, m) && (len(t) == 3 || isWS(t[3]))
}

// blankOrComment reports whether s holds only whitespace and an optional
// comment. s must start at whitespace or at the beginning of a token slot.
func blankOrComment(s string) bool {
	s = strings.TrimLeft(s, " \t")
	return s == "" || s[0] == '#'
}

// scanLines splits src into content lines, dropping blank and comment-only
// lines and handling document markers.
func scanLines(src string) ([]srcLine, error) {
	src = strings.TrimPrefix(src, "\ufeff")
	raw := strings.Split(src, "\n")
	var out []srcLine
	started, ended := false, false
	for idx, t := range raw {
		t = strings.TrimSuffix(t, "\r")
		ln := srcLine{num: idx + 1, text: t}
		n := 0
		for n < len(t) && t[n] == ' ' {
			n++
		}
		ln.indent = n
		rest := t[n:]
		if blankOrComment(rest) {
			continue
		}
		if rest[0] == '\t' {
			return nil, errAt(&ln, n, "tab character in indentation")
		}
		if n == 0 && isMarker(t, "---") {
			if started || len(out) > 0 || ended {
				return nil, errAt(&ln, 0, "multiple documents are not supported")
			}
			if !blankOrComment(t[3:]) {
				return nil, errAt(&ln, 4, "content on the '---' line is not supported")
			}
			started = true
			continue
		}
		if n == 0 && isMarker(t, "...") {
			if ended {
				return nil, errAt(&ln, 0, "multiple documents are not supported")
			}
			if !blankOrComment(t[3:]) {
				return nil, errAt(&ln, 4, "content on the '...' line is not supported")
			}
			ended = true
			continue
		}
		if ended {
			return nil, errAt(&ln, n, "content after '...': multiple documents are not supported")
		}
		out = append(out, ln)
	}
	return out, nil
}

func (p *parser) eof() bool { return p.i >= len(p.lines) }

func (p *parser) line() *srcLine { return &p.lines[p.i] }

func (p *parser) advance() {
	p.i++
	if !p.eof() {
		p.off = p.lines[p.i].indent
	}
}

func isSeqEntry(s string) bool {
	return s == "-" || strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "-\t")
}

// parseNode parses the block node that starts at the current position.
// parentCol is the indentation of the enclosing collection, or -1 at the root.
func (p *parser) parseNode(parentCol int) (*Node, error) {
	ln := p.line()
	col := p.off
	if isSeqEntry(ln.text[col:]) {
		return p.parseSeq(col)
	}
	_, _, isKey, err := splitKey(ln, col)
	if err != nil {
		return nil, err
	}
	if isKey {
		return p.parseMap(col)
	}
	n, err := parseInlineValue(ln, col)
	if err != nil {
		return nil, err
	}
	p.advance()
	if err := p.checkContinuation(parentCol, ln); err != nil {
		return nil, err
	}
	return n, nil
}

// checkContinuation rejects a line that continues a single-line value.
func (p *parser) checkContinuation(parentCol int, prev *srcLine) error {
	if p.eof() {
		return nil
	}
	ln := p.line()
	if ln.indent > parentCol {
		return errAt(ln, ln.indent, "value from line %d continues on this line: multi-line values are not supported", prev.num)
	}
	return nil
}

func (p *parser) parseSeq(col int) (*Node, error) {
	first := p.line()
	seq := &Node{Kind: Seq, Line: first.num, Col: colOf(first, col)}
	for !p.eof() {
		ln := p.line()
		c := p.off
		if c < col {
			break
		}
		if c > col {
			return nil, errAt(ln, c, "bad indentation of a sequence entry")
		}
		if !isSeqEntry(ln.text[c:]) {
			break
		}
		j := c + 1
		for j < len(ln.text) && ln.text[j] == ' ' {
			j++
		}
		if j < len(ln.text) && ln.text[j] == '\t' {
			return nil, errAt(ln, j, "tab character in indentation")
		}
		if blankOrComment(ln.text[j:]) {
			p.advance()
			if !p.eof() && p.line().indent > col {
				item, err := p.parseNode(col)
				if err != nil {
					return nil, err
				}
				seq.Items = append(seq.Items, item)
			} else {
				seq.Items = append(seq.Items, &Node{Kind: Null, Line: ln.num, Col: colOf(ln, c)})
			}
			continue
		}
		p.off = j
		item, err := p.parseNode(col)
		if err != nil {
			return nil, err
		}
		seq.Items = append(seq.Items, item)
	}
	return seq, nil
}

func (p *parser) parseMap(col int) (*Node, error) {
	first := p.line()
	m := &Node{Kind: Map, Map: map[string]*Node{}, Line: first.num, Col: colOf(first, col)}
	for !p.eof() {
		ln := p.line()
		c := p.off
		if c < col {
			break
		}
		if c > col {
			return nil, errAt(ln, c, "bad indentation of a mapping entry")
		}
		if isSeqEntry(ln.text[c:]) {
			return nil, errAt(ln, c, "expected a mapping key, found a sequence entry")
		}
		key, after, isKey, err := splitKey(ln, c)
		if err != nil {
			return nil, err
		}
		if !isKey {
			return nil, errAt(ln, c, "expected a mapping key (key: value)")
		}
		if _, dup := m.Map[key]; dup {
			return nil, errAt(ln, c, "duplicate key %q", key)
		}
		j := skipWS(ln.text, after)
		var val *Node
		if blankOrComment(ln.text[j:]) {
			p.advance()
			switch {
			case !p.eof() && p.line().indent > col:
				val, err = p.parseNode(col)
			case !p.eof() && p.line().indent == col && isSeqEntry(p.line().text[col:]):
				val, err = p.parseSeq(col)
			default:
				val = &Node{Kind: Null, Line: ln.num, Col: colOf(ln, c)}
			}
			if err != nil {
				return nil, err
			}
		} else {
			if isSeqEntry(ln.text[j:]) {
				return nil, errAt(ln, j, "a sequence entry cannot start on the same line as its key")
			}
			val, err = parseInlineValue(ln, j)
			if err != nil {
				return nil, err
			}
			p.advance()
			if err := p.checkContinuation(col, ln); err != nil {
				return nil, err
			}
		}
		m.Keys = append(m.Keys, key)
		m.Map[key] = val
	}
	return m, nil
}

// checkPlainStart rejects indicators that may not start a plain scalar.
func checkPlainStart(ln *srcLine, off int, flow bool) error {
	t := ln.text
	c := t[off]
	next := byte(' ')
	if off+1 < len(t) {
		next = t[off+1]
	}
	switch c {
	case '&':
		return errAt(ln, off, "anchors are not supported")
	case '*':
		return errAt(ln, off, "aliases are not supported")
	case '!':
		return errAt(ln, off, "tags are not supported")
	case '|', '>':
		return errAt(ln, off, "block scalars (%q) are not supported", string(c))
	case '%':
		return errAt(ln, off, "directives are not supported")
	case '@', '`':
		return errAt(ln, off, "reserved character %q cannot start a plain value", string(c))
	case '?':
		if isWS(next) || (flow && strings.IndexByte(",[]{}", next) >= 0) {
			return errAt(ln, off, "complex keys ('?') are not supported")
		}
	case ',', ']', '}', '#':
		return errAt(ln, off, "unexpected %q", string(c))
	case ':':
		if isWS(next) || (flow && strings.IndexByte(",[]{}", next) >= 0) {
			return errAt(ln, off, "unexpected ':'")
		}
	case '-':
		if flow && isWS(next) {
			return errAt(ln, off, "block sequence entries are not allowed inside flow collections")
		}
	}
	return nil
}

// splitKey reports whether the block content at off is a "key: ..." entry.
// after is the offset just past the ':'.
func splitKey(ln *srcLine, off int) (key string, after int, isKey bool, err error) {
	t := ln.text
	switch t[off] {
	case '"', '\'':
		v, end, err := parseQuoted(ln, off)
		if err != nil {
			return "", 0, false, err
		}
		j := skipWS(t, end)
		if j < len(t) && t[j] == ':' && (j+1 == len(t) || isWS(t[j+1])) {
			return v, j + 1, true, nil
		}
		return "", 0, false, nil
	case '[', '{':
		return "", 0, false, nil
	}
	if err := checkPlainStart(ln, off, false); err != nil {
		return "", 0, false, err
	}
	for j := off; j < len(t); j++ {
		switch t[j] {
		case ':':
			if j+1 == len(t) || isWS(t[j+1]) {
				k := strings.TrimRight(t[off:j], " \t")
				if k == "<<" {
					return "", 0, false, errAt(ln, off, "merge keys ('<<') are not supported")
				}
				return k, j + 1, true, nil
			}
		case '#':
			if j > off && isWS(t[j-1]) {
				return "", 0, false, nil
			}
		}
	}
	return "", 0, false, nil
}

// parseInlineValue parses a scalar or flow value that fills the rest of the line.
func parseInlineValue(ln *srcLine, off int) (*Node, error) {
	t := ln.text
	switch t[off] {
	case '"', '\'':
		v, end, err := parseQuoted(ln, off)
		if err != nil {
			return nil, err
		}
		if err := checkLineEnd(ln, end); err != nil {
			return nil, err
		}
		return &Node{Kind: Scalar, Value: v, Quoted: true, Line: ln.num, Col: colOf(ln, off)}, nil
	case '[', '{':
		n, end, err := parseFlow(ln, off)
		if err != nil {
			return nil, err
		}
		j := skipWS(t, end)
		if j < len(t) && t[j] == ':' {
			return nil, errAt(ln, off, "complex keys are not supported")
		}
		if err := checkLineEnd(ln, end); err != nil {
			return nil, err
		}
		return n, nil
	}
	if err := checkPlainStart(ln, off, false); err != nil {
		return nil, err
	}
	end := len(t)
	for j := off; j < len(t); j++ {
		if t[j] == '#' && j > off && isWS(t[j-1]) {
			end = j
			break
		}
		if t[j] == ':' && (j+1 == len(t) || isWS(t[j+1])) {
			return nil, errAt(ln, j, "nested mapping on one line is not supported (quote the value if ': ' is part of it)")
		}
	}
	return plainNode(ln, off, strings.TrimRight(t[off:end], " \t")), nil
}

func plainNode(ln *srcLine, off int, v string) *Node {
	switch v {
	case "null", "Null", "NULL", "~":
		return &Node{Kind: Null, Line: ln.num, Col: colOf(ln, off)}
	}
	return &Node{Kind: Scalar, Value: v, Line: ln.num, Col: colOf(ln, off)}
}

// checkLineEnd requires only whitespace and an optional comment from off on.
func checkLineEnd(ln *srcLine, off int) error {
	t := ln.text
	j := skipWS(t, off)
	if j == len(t) {
		return nil
	}
	if t[j] == '#' && j > off {
		return nil
	}
	return errAt(ln, j, "unexpected content %q after value", t[j:])
}

func parseQuoted(ln *srcLine, off int) (string, int, error) {
	if ln.text[off] == '\'' {
		return parseSingle(ln, off)
	}
	return parseDouble(ln, off)
}

func parseSingle(ln *srcLine, off int) (string, int, error) {
	t := ln.text
	var b strings.Builder
	for j := off + 1; j < len(t); j++ {
		if t[j] == '\'' {
			if j+1 < len(t) && t[j+1] == '\'' {
				b.WriteByte('\'')
				j++
				continue
			}
			return b.String(), j + 1, nil
		}
		b.WriteByte(t[j])
	}
	return "", 0, errAt(ln, off, "unterminated single-quoted value (quoted values must close on the same line)")
}

func parseDouble(ln *srcLine, off int) (string, int, error) {
	t := ln.text
	var b strings.Builder
	j := off + 1
	for j < len(t) {
		c := t[j]
		if c == '"' {
			return b.String(), j + 1, nil
		}
		if c != '\\' {
			b.WriteByte(c)
			j++
			continue
		}
		if j+1 >= len(t) {
			break
		}
		switch t[j+1] {
		case '"':
			b.WriteByte('"')
			j += 2
		case '\\':
			b.WriteByte('\\')
			j += 2
		case 'n':
			b.WriteByte('\n')
			j += 2
		case 't':
			b.WriteByte('\t')
			j += 2
		case 'u':
			r, ok := hex4(t, j+2)
			if !ok {
				return "", 0, errAt(ln, j, "invalid \\u escape: expected 4 hex digits")
			}
			j += 6
			if utf16.IsSurrogate(r) {
				r2, ok2 := rune(0), false
				if j+1 < len(t) && t[j] == '\\' && t[j+1] == 'u' {
					r2, ok2 = hex4(t, j+2)
				}
				dec := utf16.DecodeRune(r, r2)
				if !ok2 || dec == utf8.RuneError {
					return "", 0, errAt(ln, j-6, "invalid \\u escape: unpaired surrogate")
				}
				r = dec
				j += 6
			}
			b.WriteRune(r)
		default:
			r, _ := utf8.DecodeRuneInString(t[j+1:])
			return "", 0, errAt(ln, j, "unsupported escape \\%c", r)
		}
	}
	return "", 0, errAt(ln, off, "unterminated double-quoted value (quoted values must close on the same line)")
}

func hex4(t string, j int) (rune, bool) {
	if j+4 > len(t) {
		return 0, false
	}
	v, err := strconv.ParseUint(t[j:j+4], 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(v), true
}

func unclosed(ln *srcLine, off int) error {
	kind := "sequence"
	if ln.text[off] == '{' {
		kind = "mapping"
	}
	return errAt(ln, off, "flow %s is not closed on this line (flow values must open and close on the same line)", kind)
}

// atFlowEnd reports whether j is past the content of a flow collection.
func atFlowEnd(t string, j int) bool {
	return j >= len(t) || (t[j] == '#' && j > 0 && isWS(t[j-1]))
}

// parseFlow parses a single-line flow collection starting at off.
func parseFlow(ln *srcLine, off int) (*Node, int, error) {
	t := ln.text
	if t[off] == '[' {
		n := &Node{Kind: Seq, Items: []*Node{}, Line: ln.num, Col: colOf(ln, off)}
		j := off + 1
		for {
			j = skipWS(t, j)
			if atFlowEnd(t, j) {
				return nil, 0, unclosed(ln, off)
			}
			if t[j] == ']' {
				return n, j + 1, nil
			}
			item, end, err := parseFlowNode(ln, j)
			if err != nil {
				return nil, 0, err
			}
			n.Items = append(n.Items, item)
			j = skipWS(t, end)
			if atFlowEnd(t, j) {
				return nil, 0, unclosed(ln, off)
			}
			switch t[j] {
			case ',':
				j++
			case ']':
				return n, j + 1, nil
			case ':':
				return nil, 0, errAt(ln, j, "mappings inside flow sequences are not supported")
			default:
				return nil, 0, errAt(ln, j, "expected ',' or ']' in flow sequence")
			}
		}
	}
	n := &Node{Kind: Map, Map: map[string]*Node{}, Line: ln.num, Col: colOf(ln, off)}
	j := off + 1
	for {
		j = skipWS(t, j)
		if atFlowEnd(t, j) {
			return nil, 0, unclosed(ln, off)
		}
		if t[j] == '}' {
			return n, j + 1, nil
		}
		keyOff := j
		var key string
		if t[j] == '"' || t[j] == '\'' {
			v, end, err := parseQuoted(ln, j)
			if err != nil {
				return nil, 0, err
			}
			key, j = v, end
		} else {
			if t[j] == '[' || t[j] == '{' {
				return nil, 0, errAt(ln, j, "complex keys are not supported")
			}
			if err := checkPlainStart(ln, j, true); err != nil {
				return nil, 0, err
			}
			end := scanPlainFlow(t, j)
			key = strings.TrimRight(t[j:end], " \t")
			if key == "<<" {
				return nil, 0, errAt(ln, j, "merge keys ('<<') are not supported")
			}
			j = end
		}
		j = skipWS(t, j)
		if atFlowEnd(t, j) {
			return nil, 0, unclosed(ln, off)
		}
		if t[j] != ':' {
			return nil, 0, errAt(ln, j, "expected ':' after key %q in flow mapping", key)
		}
		if _, dup := n.Map[key]; dup {
			return nil, 0, errAt(ln, keyOff, "duplicate key %q", key)
		}
		j = skipWS(t, j+1)
		if atFlowEnd(t, j) {
			return nil, 0, unclosed(ln, off)
		}
		var val *Node
		if t[j] == ',' || t[j] == '}' {
			val = &Node{Kind: Null, Line: ln.num, Col: colOf(ln, keyOff)}
		} else {
			v, end, err := parseFlowNode(ln, j)
			if err != nil {
				return nil, 0, err
			}
			val = v
			j = skipWS(t, end)
		}
		n.Keys = append(n.Keys, key)
		n.Map[key] = val
		if atFlowEnd(t, j) {
			return nil, 0, unclosed(ln, off)
		}
		switch t[j] {
		case ',':
			j++
		case '}':
			return n, j + 1, nil
		default:
			return nil, 0, errAt(ln, j, "expected ',' or '}' in flow mapping")
		}
	}
}

func parseFlowNode(ln *srcLine, off int) (*Node, int, error) {
	t := ln.text
	switch t[off] {
	case '[', '{':
		return parseFlow(ln, off)
	case '"', '\'':
		v, end, err := parseQuoted(ln, off)
		if err != nil {
			return nil, 0, err
		}
		return &Node{Kind: Scalar, Value: v, Quoted: true, Line: ln.num, Col: colOf(ln, off)}, end, nil
	}
	if err := checkPlainStart(ln, off, true); err != nil {
		return nil, 0, err
	}
	end := scanPlainFlow(t, off)
	v := strings.TrimRight(t[off:end], " \t")
	if v == "" {
		return nil, 0, errAt(ln, off, "expected a value")
	}
	return plainNode(ln, off, v), end, nil
}

func scanPlainFlow(t string, j int) int {
	for k := j; k < len(t); k++ {
		switch t[k] {
		case ',', '[', ']', '{', '}':
			return k
		case ':':
			if k+1 == len(t) || isWS(t[k+1]) || strings.IndexByte(",[]{}", t[k+1]) >= 0 {
				return k
			}
		case '#':
			if k > j && isWS(t[k-1]) {
				return k
			}
		}
	}
	return len(t)
}
