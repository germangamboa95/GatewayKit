package transform

import (
	"bytes"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

// MapBody applies m to body. It returns mapped=false with a nil error when
// the body is empty or valid JSON that is not an object; such bodies are
// forwarded unchanged. A body that does not parse returns an error.
func MapBody(body []byte, m *config.Mapping, env Env) (out []byte, mapped bool, err error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, false, nil
	}
	v, err := DecodeJSON(trimmed)
	if err != nil {
		return nil, false, err
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, false, nil
	}
	o := newObject()
	for _, e := range m.Entries {
		if len(e.Dest) == 0 {
			continue
		}
		var b bytes.Buffer
		if e.Var != nil {
			writeString(&b, Resolve(*e.Var, env))
		} else {
			raw, ok := Get(trimmed, e.Source)
			if !ok {
				continue
			}
			writeRaw(&b, raw)
		}
		o.set(e.Dest, b.Bytes())
	}
	var b bytes.Buffer
	o.write(&b)
	return b.Bytes(), true, nil
}
