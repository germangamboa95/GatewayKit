package reqtransform

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

var reqTime = time.Date(2026, 11, 19, 19, 43, 5, 123_000_000, time.UTC)

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

func mustVar(t *testing.T, s string) *contract.Var {
	t.Helper()
	v, err := contract.ParseVar(s)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

func sampleRoute(t *testing.T) *contract.Route {
	return &contract.Route{
		Path: "/api/legacy",
		ReqTransform: &contract.ReqTransform{
			AddHeaders: []contract.HeaderAdd{
				{Name: "x-gateway", Value: *mustVar(t, "gatewaykit")},
				{Name: "x-request-start", Value: *mustVar(t, "$request_time")},
				{Name: "host", Value: *mustVar(t, "evil.example")},
			},
			RemoveHeaders: []string{"x-debug", "x-internal"},
			Mapping: &contract.Mapping{Entries: []contract.MappingEntry{
				{Dest: []string{"user", "id"}, Source: []string{"userId"}},
				{Dest: []string{"user", "name"}, Source: []string{"userName"}},
				{Dest: []string{"meta", "source"}, Var: mustVar(t, "$literal:gateway")},
				{Dest: []string{"meta", "timestamp"}, Var: mustVar(t, "$request_time")},
			}},
		},
	}
}

type recorder struct {
	called bool
	ctx    *contract.RequestContext
}

func (r *recorder) handler(ctx *contract.RequestContext) *contract.Response {
	r.called = true
	r.ctx = ctx
	return &contract.Response{Status: 200, Header: http.Header{}, FromUpstream: true}
}

func run(t *testing.T, route *contract.Route, ct, body string, hdr map[string]string) (*recorder, *contract.Response) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/legacy/v1/data", strings.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	ctx := &contract.RequestContext{Req: req, Route: route, RequestTime: reqTime, Body: []byte(body)}
	rec := &recorder{}
	resp := New(route, contract.Deps{})(rec.handler)(ctx)
	return rec, resp
}

const sampleOut = `{"user":{"id":7,"name":"Ada"},"meta":{"source":"gateway","timestamp":"2026-11-19T19:43:05.123Z"}}`

func TestSampleMappingAndHeaders(t *testing.T) {
	rec, resp := run(t, sampleRoute(t), "application/json", `{"userId": 7, "userName": "Ada", "debug": true}`,
		map[string]string{"X-Gateway": "client", "x-debug": "1", "X-Internal": "yes"})
	if !rec.called || resp.Status != 200 {
		t.Fatal("next not called")
	}
	h := rec.ctx.Req.Header
	if got := h.Values("X-Gateway"); len(got) != 1 || got[0] != "gatewaykit" {
		t.Errorf("X-Gateway = %v", got)
	}
	if got := h.Get("X-Request-Start"); got != "2026-11-19T19:43:05.123Z" {
		t.Errorf("X-Request-Start = %q", got)
	}
	if h.Get("X-Debug") != "" || h.Get("X-Internal") != "" {
		t.Errorf("removed headers still present: %v", h)
	}
	if h.Get("Host") != "" || rec.ctx.Req.Host != "example.com" {
		t.Errorf("host changed: header %q, req %q", h.Get("Host"), rec.ctx.Req.Host)
	}
	if got := string(rec.ctx.Body); got != sampleOut {
		t.Errorf("body = %s", got)
	}
	if got, want := h.Get("Content-Length"), strconv.Itoa(len(sampleOut)); got != want {
		t.Errorf("Content-Length header = %s, want %s", got, want)
	}
	if rec.ctx.Req.ContentLength != int64(len(sampleOut)) {
		t.Errorf("ContentLength = %d", rec.ctx.Req.ContentLength)
	}
	b, _ := io.ReadAll(rec.ctx.Req.Body)
	if string(b) != sampleOut {
		t.Errorf("Req.Body = %s", b)
	}
}

func TestInvalidJSON(t *testing.T) {
	for _, body := range []string{`{"userId":`, `{"a":1}x`, `{"a":1}{"b":2}`} {
		rec, resp := run(t, sampleRoute(t), "application/json", body, nil)
		if rec.called {
			t.Errorf("%q: next was called", body)
		}
		if resp.Status != 400 || resp.FromUpstream {
			t.Errorf("%q: status %d", body, resp.Status)
		}
		if got := string(resp.Body); got != `{"error":"invalid_json_body","message":"request body is not valid JSON"}` {
			t.Errorf("%q: body %s", body, got)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type %q", resp.Header.Get("Content-Type"))
		}
	}
}

func TestUnchangedBodies(t *testing.T) {
	cases := []struct{ ct, body string }{
		{"application/x-www-form-urlencoded", "a=1"},
		{"application/json", "[1, 2]"},
		{"application/json", ""},
		{"", `{"userId": 7}`},
		{"text/plain", `{"userId":`},
	}
	for _, c := range cases {
		rec, _ := run(t, sampleRoute(t), c.ct, c.body, nil)
		if !rec.called {
			t.Fatalf("%q: next not called", c.body)
		}
		if got := string(rec.ctx.Body); got != c.body {
			t.Errorf("%s %q: body %q", c.ct, c.body, got)
		}
		if got := rec.ctx.Req.Header.Get("Content-Length"); got != strconv.Itoa(len(c.body)) {
			t.Errorf("%q: Content-Length %s", c.body, got)
		}
	}
}

func TestSuffixJSONContentType(t *testing.T) {
	for _, ct := range []string{"application/vnd.api+json", "application/json; charset=utf-8", "Application/JSON"} {
		rec, _ := run(t, sampleRoute(t), ct, `{"userId": 7}`, nil)
		want := `{"user":{"id":7},"meta":{"source":"gateway","timestamp":"2026-11-19T19:43:05.123Z"}}`
		if got := string(rec.ctx.Body); got != want {
			t.Errorf("%s: body %s", ct, got)
		}
	}
}

func TestNullAndArraySources(t *testing.T) {
	route := &contract.Route{Path: "/x", ReqTransform: &contract.ReqTransform{Mapping: &contract.Mapping{
		Entries: []contract.MappingEntry{
			{Dest: []string{"first", "sku"}, Source: []string{"items", "0", "sku"}},
			{Dest: []string{"id"}, Source: []string{"userId"}},
		},
	}}}
	rec, _ := run(t, route, "application/json", `{"items": [{"sku": "A1"}], "userId": null}`, nil)
	if got := string(rec.ctx.Body); got != `{"first":{"sku":"A1"},"id":null}` {
		t.Errorf("body %s", got)
	}
}

func TestHeadersOnlyNoMapping(t *testing.T) {
	route := sampleRoute(t)
	route.ReqTransform.Mapping = nil
	rec, _ := run(t, route, "application/json", `{"userId":`, nil)
	if !rec.called || string(rec.ctx.Body) != `{"userId":` {
		t.Fatal("body should pass through without mapping")
	}
	if rec.ctx.Req.Header.Get("X-Gateway") != "gatewaykit" {
		t.Fatal("header not added")
	}
}

func TestPassThrough(t *testing.T) {
	route := &contract.Route{Path: "/x"}
	rec, _ := run(t, route, "application/json", `{"bad":`, map[string]string{"X-Debug": "1"})
	if !rec.called || rec.ctx.Req.Header.Get("X-Debug") != "1" || string(rec.ctx.Body) != `{"bad":` {
		t.Fatal("route without transform must pass through")
	}
}

func TestClockFallbackAndBodyRead(t *testing.T) {
	route := sampleRoute(t)
	req := httptest.NewRequest(http.MethodPost, "/api/legacy", strings.NewReader(`{"userId": 7, "userName": "Ada"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := &contract.RequestContext{Req: req, Route: route}
	rec := &recorder{}
	New(route, contract.Deps{Clock: fakeClock{reqTime}})(rec.handler)(ctx)
	if got := string(rec.ctx.Body); got != sampleOut {
		t.Fatalf("body %s", got)
	}
	if got := rec.ctx.Req.Header.Get("X-Request-Start"); got != "2026-11-19T19:43:05.123Z" {
		t.Fatalf("X-Request-Start %q", got)
	}
}

func TestIsJSON(t *testing.T) {
	yes := []string{"application/json", "application/json;charset=utf-8", " application/problem+json ", "APPLICATION/VND.API+JSON"}
	no := []string{"", "text/json", "application/jsonx", "text/plain", "application/x-www-form-urlencoded"}
	for _, ct := range yes {
		if !IsJSON(ct) {
			t.Errorf("%q should be JSON", ct)
		}
	}
	for _, ct := range no {
		if IsJSON(ct) {
			t.Errorf("%q should not be JSON", ct)
		}
	}
}
