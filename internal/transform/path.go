package transform

import (
	"bytes"
	"encoding/json"
)

// Get returns the raw JSON value at path in raw. Numeric segments index
// arrays; on objects every segment is a key. It reports false when the path
// does not exist.
func Get(raw json.RawMessage, path []string) (json.RawMessage, bool) {
	cur := bytes.TrimSpace(raw)
	for _, seg := range path {
		if len(cur) == 0 {
			return nil, false
		}
		switch cur[0] {
		case '{':
			var m map[string]json.RawMessage
			if json.Unmarshal(cur, &m) != nil {
				return nil, false
			}
			v, ok := m[seg]
			if !ok {
				return nil, false
			}
			cur = bytes.TrimSpace(v)
		case '[':
			i, ok := arrayIndex(seg)
			if !ok {
				return nil, false
			}
			var a []json.RawMessage
			if json.Unmarshal(cur, &a) != nil || i >= len(a) {
				return nil, false
			}
			cur = bytes.TrimSpace(a[i])
		default:
			return nil, false
		}
	}
	if len(cur) == 0 {
		return nil, false
	}
	return json.RawMessage(cur), true
}

func arrayIndex(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	n := 0
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
