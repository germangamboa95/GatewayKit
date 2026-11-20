package transform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

func entry(t *testing.T, dest, src string) config.MappingEntry {
	t.Helper()
	e := config.MappingEntry{Dest: strings.Split(dest, ".")}
	if strings.HasPrefix(src, "$") {
		v := mustVar(t, src)
		e.Var = &v
	} else {
		e.Source = strings.Split(src, ".")
	}
	return e
}

func sampleMapping(t *testing.T) *config.Mapping {
	return &config.Mapping{Entries: []config.MappingEntry{
		entry(t, "user.id", "userId"),
		entry(t, "user.name", "userName"),
		entry(t, "meta.source", "$literal:gateway"),
		entry(t, "meta.timestamp", "$request_time"),
	}}
}

func mapString(t *testing.T, m *config.Mapping, body string) (string, bool) {
	t.Helper()
	out, mapped, err := MapBody([]byte(body), m, Env{RequestTime: reqTime, RoutePath: "/api/legacy"})
	if err != nil {
		t.Fatalf("MapBody(%q): %v", body, err)
	}
	return string(out), mapped
}

func TestMapBodySpecExample(t *testing.T) {
	got, mapped := mapString(t, sampleMapping(t), `{"userId": 7, "userName": "Ada", "debug": true}`)
	want := `{"user":{"id":7,"name":"Ada"},"meta":{"source":"gateway","timestamp":"2026-11-19T19:43:05.123Z"}}`
	if !mapped || got != want {
		t.Fatalf("got %s (mapped=%v), want %s", got, mapped, want)
	}
}

func TestMapBodyMissingSource(t *testing.T) {
	got, _ := mapString(t, sampleMapping(t), `{"userId": 7}`)
	want := `{"user":{"id":7},"meta":{"source":"gateway","timestamp":"2026-11-19T19:43:05.123Z"}}`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestMapBodyNullSource(t *testing.T) {
	got, _ := mapString(t, sampleMapping(t), `{"userId": null}`)
	want := `{"user":{"id":null},"meta":{"source":"gateway","timestamp":"2026-11-19T19:43:05.123Z"}}`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestMapBodyAllMissingOmitsParent(t *testing.T) {
	m := &config.Mapping{Entries: []config.MappingEntry{entry(t, "user.id", "userId")}}
	got, mapped := mapString(t, m, `{}`)
	if !mapped || got != `{}` {
		t.Fatalf("got %s mapped=%v", got, mapped)
	}
}

func TestMapBodyArrayIndex(t *testing.T) {
	m := &config.Mapping{Entries: []config.MappingEntry{entry(t, "first.sku", "items.0.sku")}}
	got, _ := mapString(t, m, `{"items": [{"sku": "A1"}]}`)
	if got != `{"first":{"sku":"A1"}}` {
		t.Fatalf("got %s", got)
	}
	got, _ = mapString(t, m, `{"items": []}`)
	if got != `{}` {
		t.Fatalf("out of range: got %s", got)
	}
}

func TestMapBodyFidelity(t *testing.T) {
	m := &config.Mapping{Entries: []config.MappingEntry{
		entry(t, "big", "n"),
		entry(t, "obj", "o"),
		entry(t, "html", "h"),
	}}
	got, _ := mapString(t, m, `{"n": 12345678901234567890.50, "o": {"z": 1, "a": [1, 2.0e3]}, "h": "<a&b>"}`)
	want := `{"big":12345678901234567890.50,"obj":{"z":1,"a":[1,2.0e3]},"html":"<a&b>"}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestMapBodyNumericKeyOnObject(t *testing.T) {
	m := &config.Mapping{Entries: []config.MappingEntry{entry(t, "x", "o.0")}}
	got, _ := mapString(t, m, `{"o": {"0": "zero"}}`)
	if got != `{"x":"zero"}` {
		t.Fatalf("got %s", got)
	}
}

func TestMapBodyPassThrough(t *testing.T) {
	for _, body := range []string{"", "  ", "[1, 2]", `"s"`, "7", "null"} {
		out, mapped, err := MapBody([]byte(body), sampleMapping(t), Env{})
		if err != nil || mapped || out != nil {
			t.Errorf("%q: out=%q mapped=%v err=%v", body, out, mapped, err)
		}
	}
}

func TestMapBodyInvalid(t *testing.T) {
	for _, body := range []string{`{"userId":`, `{"a":1}x`, `{"a":1} {"b":2}`, `{"a":1}]`, `nope`} {
		if _, _, err := MapBody([]byte(body), sampleMapping(t), Env{}); err == nil {
			t.Errorf("%q: want error", body)
		}
	}
}

func TestDecodeJSON(t *testing.T) {
	v, err := DecodeJSON([]byte(" {\"a\": 7} \n"))
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := v.(map[string]any)["a"].(json.Number); !ok || n.String() != "7" {
		t.Fatalf("got %#v", v)
	}
	if _, err := DecodeJSON([]byte(`{"a":1}x`)); err != ErrTrailingData {
		t.Fatalf("trailing: got %v", err)
	}
	if _, err := DecodeJSON(nil); err == nil {
		t.Fatal("empty: want error")
	}
}

func TestGet(t *testing.T) {
	raw := json.RawMessage(`{"a": {"b": [10, {"c": null}]}, "s": "x"}`)
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"a.b.0", "10", true},
		{"a.b.1.c", "null", true},
		{"a.b.2", "", false},
		{"a.b.x", "", false},
		{"a.b.-1", "", false},
		{"s.0", "", false},
		{"missing", "", false},
		{"a.b.1", `{"c": null}`, true},
	}
	for _, c := range cases {
		got, ok := Get(raw, strings.Split(c.path, "."))
		if ok != c.ok || string(got) != c.want {
			t.Errorf("Get(%s) = %q,%v want %q,%v", c.path, got, ok, c.want, c.ok)
		}
	}
}
