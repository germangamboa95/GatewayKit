// Package yamlparse parses the YAML subset used by gateway.yaml into an
// untyped, positioned node tree.
package yamlparse

import "fmt"

// Kind is the type of a Node.
type Kind int

const (
	Map Kind = iota
	Seq
	Scalar
	Null
)

func (k Kind) String() string {
	switch k {
	case Map:
		return "map"
	case Seq:
		return "list"
	case Scalar:
		return "string"
	case Null:
		return "null"
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Node is one value in the parsed tree. Line and Col are 1-based.
type Node struct {
	Kind   Kind
	Value  string
	Quoted bool
	Keys   []string
	Map    map[string]*Node
	Items  []*Node
	Line   int
	Col    int
	// KeyPos holds the position of each map key.
	KeyPos map[string]Pos
}

// Pos is a 1-based line and column.
type Pos struct {
	Line int
	Col  int
}

// Error is a parse error at a 1-based line and column.
type Error struct {
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg)
}
