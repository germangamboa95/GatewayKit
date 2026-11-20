package config

import (
	"fmt"
	"strings"
)

// VarKind identifies a compiled transform value (config spec §9 "Variables").
type VarKind int

const (
	VarLiteral VarKind = iota // plain string or $literal:<text>; Var.Text holds the value
	VarRequestTime
	VarResponseTime
	VarRoutePath
	VarBody
)

// Var is a transform string compiled at load time.
type Var struct {
	Kind VarKind
	Text string
}

// Literal returns a literal Var.
func Literal(s string) Var { return Var{Kind: VarLiteral, Text: s} }

// ParseVar compiles a whole-value token. Strings not starting with "$" are
// literals. Scope checks (where a token may appear) are the loader's job.
func ParseVar(s string) (Var, error) {
	if !strings.HasPrefix(s, "$") {
		return Literal(s), nil
	}
	if text, ok := strings.CutPrefix(s, "$literal:"); ok {
		return Literal(text), nil
	}
	switch s {
	case "$request_time":
		return Var{Kind: VarRequestTime}, nil
	case "$response_time":
		return Var{Kind: VarResponseTime}, nil
	case "$route_path":
		return Var{Kind: VarRoutePath}, nil
	case "$body":
		return Var{Kind: VarBody}, nil
	}
	return Var{}, fmt.Errorf("%q is not a known variable", s)
}

// HeaderAdd is one headers.add entry. Name is lowercase.
type HeaderAdd struct {
	Name  string
	Value Var
}

// ReqTransform removes headers, adds headers, then maps the body.
type ReqTransform struct {
	AddHeaders    []HeaderAdd // YAML order
	RemoveHeaders []string    // lowercase
	Mapping       *Mapping    // nil when absent
}

// RespTransform wraps the body in an envelope, removes headers, then adds headers.
type RespTransform struct {
	AddHeaders    []HeaderAdd
	RemoveHeaders []string
	Envelope      *Template // nil when absent
}

// Mapping is a compiled request body mapping, entries in YAML order.
type Mapping struct {
	Entries []MappingEntry
}

// MappingEntry is one body mapping. Exactly one of Source and Var is set.
type MappingEntry struct {
	Dest   []string // destination dot-path segments, all object keys
	Source []string // source dot-path segments; numeric segments may index arrays
	Var    *Var     // set when the value is a $token
}

// TemplateKind identifies an envelope template node.
type TemplateKind int

const (
	TemplateObject TemplateKind = iota
	TemplateArray
	TemplateVar    // string leaf: literal or variable, including $body
	TemplateScalar // number, bool or null, copied as JSON text in Raw
)

// Template is a compiled envelope as an ordered tree.
type Template struct {
	Kind   TemplateKind
	Fields []TemplateField // TemplateObject, YAML order
	Items  []*Template     // TemplateArray
	Var    Var             // TemplateVar
	Raw    string          // TemplateScalar, e.g. "true", "3", "null"
}

// TemplateField is one key of a TemplateObject.
type TemplateField struct {
	Key   string
	Value *Template
}
