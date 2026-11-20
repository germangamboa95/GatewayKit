package yamlparse

import "errors"

type Kind int

const (
	Null Kind = iota
	Scalar
	Map
	Seq
)

type Node struct {
	Kind      Kind
	Value     string
	Quoted    bool
	Keys      []string
	Map       map[string]*Node
	Items     []*Node
	Line, Col int
}

func ParseYAML(src []byte) (*Node, error) {
	return nil, errors.New("yamlparse: not implemented")
}
