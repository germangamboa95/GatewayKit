package transform

import (
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

func tvar(t *testing.T, s string) *config.Template {
	return &config.Template{Kind: config.TemplateVar, Var: mustVar(t, s)}
}

func tobj(fields ...config.TemplateField) *config.Template {
	return &config.Template{Kind: config.TemplateObject, Fields: fields}
}

func field(k string, v *config.Template) config.TemplateField {
	return config.TemplateField{Key: k, Value: v}
}

func sampleEnvelope(t *testing.T) *config.Template {
	return tobj(
		field("data", tvar(t, "$body")),
		field("gateway_metadata", tobj(
			field("served_at", tvar(t, "$response_time")),
			field("route", tvar(t, "$route_path")),
		)),
	)
}

var respEnv = Env{
	RequestTime:  reqTime,
	ResponseTime: time.Date(2026, 11, 19, 19, 43, 6, 0, time.UTC),
	RoutePath:    "/api/legacy",
}

func TestEnvelopeSample(t *testing.T) {
	got := string(Envelope(sampleEnvelope(t), []byte(`{"ok": 1}`), respEnv))
	want := `{"data":{"ok":1},"gateway_metadata":{"served_at":"2026-11-19T19:43:06.000Z","route":"/api/legacy"}}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestEnvelopeVariantB(t *testing.T) {
	tpl := tobj(
		field("result", tobj(
			field("payload", tvar(t, "$body")),
			field("ok", &config.Template{Kind: config.TemplateScalar, Raw: "true"}),
		)),
		field("route", tvar(t, "$route_path")),
	)
	got := string(Envelope(tpl, []byte("[1,2]"), Env{RoutePath: "/v2/admin"}))
	want := `{"result":{"payload":[1,2],"ok":true},"route":"/v2/admin"}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestEnvelopeBodyKinds(t *testing.T) {
	tpl := tobj(field("data", tvar(t, "$body")))
	cases := map[string]string{
		`{"id": 7}`:               `{"data":{"id":7}}`,
		"hello":                   `{"data":"hello"}`,
		"":                        `{"data":null}`,
		`{"z":1,"a":1.50}`:        `{"data":{"z":1,"a":1.50}}`,
		`{"a":1}x`:                `{"data":"{\"a\":1}x"}`,
		"<b>&</b>":                `{"data":"<b>&</b>"}`,
		" [ 1 , 2 ] \n":           `{"data":[1,2]}`,
		`"str"`:                   `{"data":"str"}`,
		"null":                    `{"data":null}`,
		`{"e": 1}`:                `{"data":{"e":1}}`,
		"line1\nline2\t\"quote\"": `{"data":"line1\nline2\t\"quote\""}`,
	}
	for body, want := range cases {
		if got := string(Envelope(tpl, []byte(body), respEnv)); got != want {
			t.Errorf("body %q: got %s, want %s", body, got, want)
		}
	}
}

func TestEnvelopeArraysAndScalars(t *testing.T) {
	tpl := tobj(
		field("list", &config.Template{Kind: config.TemplateArray, Items: []*config.Template{
			{Kind: config.TemplateScalar, Raw: "3"},
			{Kind: config.TemplateScalar, Raw: "null"},
			tvar(t, "$literal:$x"),
			tobj(field("b", tvar(t, "$body"))),
		}}),
		field("v", tvar(t, "v=$request_time")),
		field("t", tvar(t, "$request_time")),
		field("f", &config.Template{Kind: config.TemplateScalar, Raw: "false"}),
	)
	got := string(Envelope(tpl, []byte("1"), respEnv))
	want := `{"list":[3,null,"$x",{"b":1}],"v":"v=$request_time","t":"2026-11-19T19:43:05.123Z","f":false}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
