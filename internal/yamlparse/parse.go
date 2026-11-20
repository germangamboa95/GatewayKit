package yamlparse

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// maxExpanded caps the nodes that alias expansion may create, so a small
// file of nested aliases cannot expand into an enormous tree.
const maxExpanded = 100_000

// ParseYAML parses src with gopkg.in/yaml.v3 and converts the single document
// into a Node tree. Aliases are expanded and merge keys ("<<") are applied.
// Errors are *Error values carrying the line and, when known, the column.
func ParseYAML(src []byte) (*Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return &Node{Kind: Null, Line: 1, Col: 1}, nil
		}
		return nil, wrapErr(err)
	}
	var next yaml.Node
	switch err := dec.Decode(&next); {
	case err == nil:
		return nil, errAt(&next, "multiple documents are not supported")
	case !errors.Is(err, io.EOF):
		return nil, wrapErr(err)
	}
	if len(doc.Content) == 0 {
		return &Node{Kind: Null, Line: 1, Col: 1}, nil
	}
	c := &converter{active: map[*yaml.Node]bool{}}
	return c.node(doc.Content[0])
}

var lineErr = regexp.MustCompile(`(?s)^yaml: line (\d+): (.*)$`)

// wrapErr turns a yaml.v3 error into an *Error. yaml.v3 reports a line but
// no column, and reader errors (bad UTF-8, control characters) report neither.
func wrapErr(err error) *Error {
	msg := err.Error()
	if m := lineErr.FindStringSubmatch(msg); m != nil {
		line, _ := strconv.Atoi(m[1])
		return &Error{Line: line, Msg: m[2]}
	}
	if len(msg) > 6 && msg[:6] == "yaml: " {
		msg = msg[6:]
	}
	return &Error{Msg: msg}
}

func errAt(y *yaml.Node, format string, args ...any) *Error {
	return &Error{Line: y.Line, Col: y.Column, Msg: fmt.Sprintf(format, args...)}
}

type converter struct {
	active   map[*yaml.Node]bool // anchored nodes being expanded, to catch cycles
	inAlias  int                 // depth of alias expansion
	expanded int                 // nodes created inside alias expansion
}

// target follows an alias to its anchored node.
func (c *converter) target(y *yaml.Node) (*yaml.Node, error) {
	if y.Kind != yaml.AliasNode {
		return y, nil
	}
	if c.active[y.Alias] {
		return nil, errAt(y, "alias *%s refers to a node that contains it", y.Value)
	}
	return y.Alias, nil
}

func (c *converter) node(y *yaml.Node) (*Node, error) {
	if y.Kind == yaml.AliasNode {
		t, err := c.target(y)
		if err != nil {
			return nil, err
		}
		c.active[t] = true
		c.inAlias++
		n, err := c.node(t)
		c.inAlias--
		delete(c.active, t)
		return n, err
	}
	if c.inAlias > 0 {
		if c.expanded++; c.expanded > maxExpanded {
			return nil, errAt(y, "aliases expand to more than %d nodes", maxExpanded)
		}
	}
	if y.Style&yaml.TaggedStyle != 0 && !(y.Kind == yaml.ScalarNode && y.Tag == "!!str") {
		return nil, errAt(y, "tag %s is not supported (only !!str is)", y.Tag)
	}
	switch y.Kind {
	case yaml.ScalarNode:
		return scalar(y), nil
	case yaml.SequenceNode:
		s := &Node{Kind: Seq, Line: y.Line, Col: y.Column}
		for _, it := range y.Content {
			n, err := c.node(it)
			if err != nil {
				return nil, err
			}
			s.Items = append(s.Items, n)
		}
		return s, nil
	case yaml.MappingNode:
		return c.mapping(y)
	}
	return nil, errAt(y, "unexpected YAML node kind %d", y.Kind)
}

func scalar(y *yaml.Node) *Node {
	quoted := y.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) != 0
	if !quoted && y.ShortTag() == "!!null" {
		return &Node{Kind: Null, Line: y.Line, Col: y.Column}
	}
	return &Node{Kind: Scalar, Value: y.Value, Quoted: quoted, Line: y.Line, Col: y.Column}
}

func (c *converter) mapping(y *yaml.Node) (*Node, error) {
	m := &Node{Kind: Map, Map: map[string]*Node{}, KeyPos: map[string]Pos{}, Line: y.Line, Col: y.Column}
	var merges []int
	for i := 0; i+1 < len(y.Content); i += 2 {
		k, v := y.Content[i], y.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.ShortTag() == "!!merge" {
			merges = append(merges, i)
			continue
		}
		key, err := c.key(k)
		if err != nil {
			return nil, err
		}
		if _, dup := m.Map[key]; dup {
			return nil, errAt(k, "duplicate key %q", key)
		}
		val, err := c.node(v)
		if err != nil {
			return nil, err
		}
		// An empty value sits at the key, as `key:` has nothing else to point at.
		if val.Kind == Null && v.Kind == yaml.ScalarNode && v.Value == "" {
			val.Line, val.Col = k.Line, k.Column
		}
		m.Keys = append(m.Keys, key)
		m.Map[key] = val
		m.KeyPos[key] = Pos{Line: k.Line, Col: k.Column}
	}
	for _, i := range merges {
		if err := c.merge(m, y.Content[i], y.Content[i+1]); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (c *converter) key(k *yaml.Node) (string, error) {
	t, err := c.target(k)
	if err != nil {
		return "", err
	}
	if t.Kind != yaml.ScalarNode {
		return "", errAt(k, "complex keys are not supported")
	}
	if t.Style&yaml.TaggedStyle != 0 && t.Tag != "!!str" {
		return "", errAt(k, "tag %s is not supported (only !!str is)", t.Tag)
	}
	return t.Value, nil
}

// merge applies the value of the "<<" key k to m. Keys already in m win, and
// within a list of maps the earlier map wins. Converting the value through
// c.node applies the alias cycle check and the expansion cap.
func (c *converter) merge(m *Node, k, v *yaml.Node) error {
	src, err := c.node(v)
	if err != nil {
		return err
	}
	srcs := []*Node{src}
	if src.Kind == Seq {
		srcs = src.Items
	}
	for _, s := range srcs {
		if s.Kind != Map {
			return errAt(k, "merge key '<<' needs a map or a list of maps")
		}
		for _, key := range s.Keys {
			if _, ok := m.Map[key]; ok {
				continue
			}
			m.Keys = append(m.Keys, key)
			m.Map[key] = s.Map[key]
			m.KeyPos[key] = s.KeyPos[key]
		}
	}
	return nil
}
