package transform

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// ErrTrailingData means a JSON value was followed by more data.
var ErrTrailingData = errors.New("unexpected data after JSON value")

// DecodeJSON decodes exactly one JSON value with UseNumber. Trailing data
// other than whitespace is an error (design D8).
func DecodeJSON(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, ErrTrailingData
	}
	return v, nil
}

func writeString(b *bytes.Buffer, s string) {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Write(bytes.TrimRight(tmp.Bytes(), "\n"))
}

func writeRaw(b *bytes.Buffer, raw []byte) {
	if err := json.Compact(b, raw); err != nil {
		b.WriteString("null")
	}
}

// object is an insertion-ordered JSON object. Values are encoded JSON
// ([]byte) or nested *object.
type object struct {
	keys []string
	vals map[string]any
}

func newObject() *object { return &object{vals: map[string]any{}} }

func (o *object) put(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// set stores v at path, creating intermediate objects as needed.
func (o *object) set(path []string, v []byte) {
	cur := o
	for _, k := range path[:len(path)-1] {
		next, ok := cur.vals[k].(*object)
		if !ok {
			next = newObject()
			cur.put(k, next)
		}
		cur = next
	}
	cur.put(path[len(path)-1], v)
}

func (o *object) write(b *bytes.Buffer) {
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		writeString(b, k)
		b.WriteByte(':')
		switch v := o.vals[k].(type) {
		case *object:
			v.write(b)
		case []byte:
			b.Write(v)
		}
	}
	b.WriteByte('}')
}
