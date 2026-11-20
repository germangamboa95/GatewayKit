package transform

import (
	"bytes"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

// BodyValue returns the JSON for $body: the body compacted when it is one
// JSON value, null when empty, otherwise the body as a JSON string.
func BodyValue(body []byte) []byte {
	var b bytes.Buffer
	switch {
	case len(body) == 0:
		b.WriteString("null")
	default:
		if _, err := DecodeJSON(body); err == nil {
			writeRaw(&b, body)
		} else {
			writeString(&b, string(body))
		}
	}
	return b.Bytes()
}

// Envelope renders t in template order with the upstream body as $body.
func Envelope(t *config.Template, body []byte, env Env) []byte {
	var bodyJSON []byte
	var b bytes.Buffer
	var walk func(t *config.Template)
	walk = func(t *config.Template) {
		if t == nil {
			b.WriteString("null")
			return
		}
		switch t.Kind {
		case config.TemplateObject:
			b.WriteByte('{')
			for i, f := range t.Fields {
				if i > 0 {
					b.WriteByte(',')
				}
				writeString(&b, f.Key)
				b.WriteByte(':')
				walk(f.Value)
			}
			b.WriteByte('}')
		case config.TemplateArray:
			b.WriteByte('[')
			for i, it := range t.Items {
				if i > 0 {
					b.WriteByte(',')
				}
				walk(it)
			}
			b.WriteByte(']')
		case config.TemplateVar:
			if t.Var.Kind == config.VarBody {
				if bodyJSON == nil {
					bodyJSON = BodyValue(body)
				}
				b.Write(bodyJSON)
			} else {
				writeString(&b, Resolve(t.Var, env))
			}
		case config.TemplateScalar:
			if t.Raw == "" {
				b.WriteString("null")
			} else {
				writeRaw(&b, []byte(t.Raw))
			}
		default:
			b.WriteString("null")
		}
	}
	walk(t)
	return b.Bytes()
}
