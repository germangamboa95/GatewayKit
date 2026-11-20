package resptransform

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

var (
	reqTime  = time.Date(2026, 11, 19, 19, 43, 5, 123_000_000, time.UTC)
	respTime = time.Date(2026, 11, 19, 19, 43, 6, 0, time.UTC)
)

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

func tvar(t *testing.T, s string) *contract.Template {
	t.Helper()
	v, err := contract.ParseVar(s)
	if err != nil {
		t.Fatal(err)
	}
	return &contract.Template{Kind: contract.TemplateVar, Var: v}
}

func sampleRoute(t *testing.T) *contract.Route {
	return &contract.Route{
		Path: "/api/legacy",
		RespTransform: &contract.RespTransform{
			AddHeaders:    []contract.HeaderAdd{{Name: "x-served-by", Value: contract.Literal("gatewaykit")}},
			RemoveHeaders: []string{"server", "x-powered-by"},
			Envelope: &contract.Template{Kind: contract.TemplateObject, Fields: []contract.TemplateField{
				{Key: "data", Value: tvar(t, "$body")},
				{Key: "gateway_metadata", Value: &contract.Template{Kind: contract.TemplateObject, Fields: []contract.TemplateField{
					{Key: "served_at", Value: tvar(t, "$response_time")},
					{Key: "route", Value: tvar(t, "$route_path")},
				}}},
			}},
		},
	}
}

func newCtx(route *contract.Route, acceptEncoding string) *contract.RequestContext {
	req := httptest.NewRequest(http.MethodGet, "/api/legacy/v1/data", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	return &contract.RequestContext{Req: req, Route: route, RequestTime: reqTime}
}

func upstream(status int, body string, hdr http.Header, seen *http.Header) contract.Handler {
	return func(ctx *contract.RequestContext) *contract.Response {
		if seen != nil {
			*seen = ctx.Req.Header.Clone()
		}
		ctx.ResponseTime = respTime
		r := &contract.Response{Status: status, Header: hdr, Body: []byte(body), FromUpstream: true}
		ctx.Upstream = r
		return r
	}
}

const sampleWant = `{"data":{"ok":1},"gateway_metadata":{"served_at":"2026-11-19T19:43:06.000Z","route":"/api/legacy"}}`

func TestSampleEnvelope(t *testing.T) {
	route := sampleRoute(t)
	var seen http.Header
	hdr := http.Header{
		"Content-Type":   {"text/plain"},
		"Content-Length": {"9"},
		"Server":         {"mock"},
		"X-Powered-By":   {"mock"},
		"X-Other":        {"keep"},
	}
	ctx := newCtx(route, "gzip")
	resp := New(route, contract.Deps{})(upstream(200, `{"ok": 1}`, hdr, &seen))(ctx)
	if got := seen.Get("Accept-Encoding"); got != "identity" {
		t.Errorf("upstream Accept-Encoding = %q", got)
	}
	if resp.Status != 200 || string(resp.Body) != sampleWant {
		t.Fatalf("got %d %s", resp.Status, resp.Body)
	}
	want := http.Header{
		"Content-Type":   {"application/json"},
		"Content-Length": {strconv.Itoa(len(sampleWant))},
		"X-Other":        {"keep"},
		"X-Served-By":    {"gatewaykit"},
	}
	if !reflect.DeepEqual(resp.Header, want) {
		t.Fatalf("headers %v, want %v", resp.Header, want)
	}
}

func TestUpstreamErrorWrapped(t *testing.T) {
	route := sampleRoute(t)
	resp := New(route, contract.Deps{})(upstream(503, `{"e": 1}`, http.Header{}, nil))(newCtx(route, ""))
	want := `{"data":{"e":1},"gateway_metadata":{"served_at":"2026-11-19T19:43:06.000Z","route":"/api/legacy"}}`
	if resp.Status != 503 || string(resp.Body) != want {
		t.Fatalf("got %d %s", resp.Status, resp.Body)
	}
}

func TestBodyKinds(t *testing.T) {
	route := sampleRoute(t)
	cases := map[string]string{
		"hello": `"hello"`,
		"":      `null`,
	}
	for body, data := range cases {
		resp := New(route, contract.Deps{})(upstream(200, body, nil, nil))(newCtx(route, ""))
		want := `{"data":` + data + `,"gateway_metadata":{"served_at":"2026-11-19T19:43:06.000Z","route":"/api/legacy"}}`
		if string(resp.Body) != want {
			t.Errorf("body %q: got %s", body, resp.Body)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Errorf("body %q: content type %q", body, resp.Header.Get("Content-Type"))
		}
	}
}

func TestVariantB(t *testing.T) {
	route := &contract.Route{Path: "/v2/admin", RespTransform: &contract.RespTransform{
		Envelope: &contract.Template{Kind: contract.TemplateObject, Fields: []contract.TemplateField{
			{Key: "result", Value: &contract.Template{Kind: contract.TemplateObject, Fields: []contract.TemplateField{
				{Key: "payload", Value: tvar(t, "$body")},
				{Key: "ok", Value: &contract.Template{Kind: contract.TemplateScalar, Raw: "true"}},
			}}},
			{Key: "route", Value: tvar(t, "$route_path")},
		}},
	}}
	resp := New(route, contract.Deps{})(upstream(200, "[1,2]", http.Header{"Content-Type": {"application/json"}}, nil))(newCtx(route, ""))
	if got := string(resp.Body); got != `{"result":{"payload":[1,2],"ok":true},"route":"/v2/admin"}` {
		t.Fatalf("got %s", got)
	}
}

func TestGatewayErrorNotWrapped(t *testing.T) {
	route := sampleRoute(t)
	next := func(ctx *contract.RequestContext) *contract.Response {
		ctx.Upstream = &contract.Response{Status: 503, Header: http.Header{}, Body: []byte(`{"e":1}`), FromUpstream: true}
		return contract.GatewayTimeout()
	}
	resp := New(route, contract.Deps{})(next)(newCtx(route, ""))
	want := contract.GatewayTimeout()
	if resp.Status != 504 || string(resp.Body) != string(want.Body) {
		t.Fatalf("got %d %s", resp.Status, resp.Body)
	}
	if !reflect.DeepEqual(resp.Header, want.Header) {
		t.Fatalf("headers %v", resp.Header)
	}
}

func TestUpstream503ThenTimeoutGivesPlain504(t *testing.T) {
	route := sampleRoute(t)
	attempts := 0
	next := func(ctx *contract.RequestContext) *contract.Response {
		var last *contract.Response
		for attempt := 1; attempt <= 2; attempt++ {
			attempts++
			if attempt == 1 {
				last = &contract.Response{Status: 503, Header: http.Header{"Server": {"mock"}}, Body: []byte(`{"e":1}`), FromUpstream: true}
				ctx.Upstream = last
				ctx.ResponseTime = respTime
				continue
			}
			last = contract.GatewayTimeout()
		}
		return last
	}
	resp := New(route, contract.Deps{})(next)(newCtx(route, ""))
	if attempts != 2 {
		t.Fatalf("attempts %d", attempts)
	}
	if resp.Status != 504 || string(resp.Body) != `{"error":"gateway_timeout","message":"upstream timed out"}` {
		t.Fatalf("got %d %s", resp.Status, resp.Body)
	}
	if resp.Header.Get("X-Served-By") != "" {
		t.Fatal("gateway error got response headers")
	}
}

func TestHeadersOnlyNoEnvelope(t *testing.T) {
	route := sampleRoute(t)
	route.RespTransform.Envelope = nil
	var seen http.Header
	resp := New(route, contract.Deps{})(upstream(200, "raw", http.Header{"Server": {"mock"}, "Content-Type": {"text/plain"}}, &seen))(newCtx(route, "gzip"))
	if seen.Get("Accept-Encoding") != "gzip" {
		t.Errorf("Accept-Encoding changed without envelope: %q", seen.Get("Accept-Encoding"))
	}
	if string(resp.Body) != "raw" || resp.Header.Get("Content-Type") != "text/plain" {
		t.Errorf("body or type changed: %s %v", resp.Body, resp.Header)
	}
	if resp.Header.Get("Server") != "" || resp.Header.Get("X-Served-By") != "gatewaykit" {
		t.Errorf("headers %v", resp.Header)
	}
}

func TestResponseTimeFallback(t *testing.T) {
	route := sampleRoute(t)
	next := func(ctx *contract.RequestContext) *contract.Response {
		return &contract.Response{Status: 200, Body: []byte(`{"ok": 1}`), FromUpstream: true}
	}
	resp := New(route, contract.Deps{Clock: fakeClock{respTime}})(next)(newCtx(route, ""))
	if string(resp.Body) != sampleWant {
		t.Fatalf("got %s", resp.Body)
	}
}

func TestPassThrough(t *testing.T) {
	route := &contract.Route{Path: "/x"}
	var seen http.Header
	hdr := http.Header{"Server": {"mock"}}
	resp := New(route, contract.Deps{})(upstream(200, "x", hdr, &seen))(newCtx(route, "gzip"))
	if seen.Get("Accept-Encoding") != "gzip" || string(resp.Body) != "x" || resp.Header.Get("Server") != "mock" {
		t.Fatal("route without transform must pass through")
	}
}
