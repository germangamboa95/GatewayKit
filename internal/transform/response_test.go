package transform

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

var respTime = time.Date(2026, 11, 19, 19, 43, 6, 0, time.UTC)

func responseRoute(t *testing.T) *config.Route {
	return &config.Route{
		Path: "/api/legacy",
		RespTransform: &config.RespTransform{
			AddHeaders:    []config.HeaderAdd{{Name: "x-served-by", Value: config.Literal("gatewaykit")}},
			RemoveHeaders: []string{"server", "x-powered-by"},
			Envelope: &config.Template{Kind: config.TemplateObject, Fields: []config.TemplateField{
				{Key: "data", Value: tvar(t, "$body")},
				{Key: "gateway_metadata", Value: &config.Template{Kind: config.TemplateObject, Fields: []config.TemplateField{
					{Key: "served_at", Value: tvar(t, "$response_time")},
					{Key: "route", Value: tvar(t, "$route_path")},
				}}},
			}},
		},
	}
}

func newCtx(route *config.Route, acceptEncoding string) *gateway.RequestContext {
	req := httptest.NewRequest(http.MethodGet, "/api/legacy/v1/data", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	return &gateway.RequestContext{Req: req, Route: route, RequestTime: reqTime}
}

func upstream(status int, body string, hdr http.Header, seen *http.Header) gateway.Handler {
	return func(rc *gateway.RequestContext) *gateway.Response {
		if seen != nil {
			*seen = rc.Req.Header.Clone()
		}
		rc.ResponseTime = respTime
		r := &gateway.Response{Status: status, Header: hdr, Body: []byte(body), FromUpstream: true}
		rc.Upstream = r
		return r
	}
}

const sampleWant = `{"data":{"ok":1},"gateway_metadata":{"served_at":"2026-11-19T19:43:06.000Z","route":"/api/legacy"}}`

func TestSampleEnvelope(t *testing.T) {
	route := responseRoute(t)
	var seen http.Header
	hdr := http.Header{
		"Content-Type":   {"text/plain"},
		"Content-Length": {"9"},
		"Server":         {"mock"},
		"X-Powered-By":   {"mock"},
		"X-Other":        {"keep"},
	}
	rc := newCtx(route, "gzip")
	resp := ResponseMiddleware(route, nil)(upstream(200, `{"ok": 1}`, hdr, &seen))(rc)
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
	route := responseRoute(t)
	resp := ResponseMiddleware(route, nil)(upstream(503, `{"e": 1}`, http.Header{}, nil))(newCtx(route, ""))
	want := `{"data":{"e":1},"gateway_metadata":{"served_at":"2026-11-19T19:43:06.000Z","route":"/api/legacy"}}`
	if resp.Status != 503 || string(resp.Body) != want {
		t.Fatalf("got %d %s", resp.Status, resp.Body)
	}
}

func TestBodyKinds(t *testing.T) {
	route := responseRoute(t)
	cases := map[string]string{
		"hello": `"hello"`,
		"":      `null`,
	}
	for body, data := range cases {
		resp := ResponseMiddleware(route, nil)(upstream(200, body, nil, nil))(newCtx(route, ""))
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
	route := &config.Route{Path: "/v2/admin", RespTransform: &config.RespTransform{
		Envelope: &config.Template{Kind: config.TemplateObject, Fields: []config.TemplateField{
			{Key: "result", Value: &config.Template{Kind: config.TemplateObject, Fields: []config.TemplateField{
				{Key: "payload", Value: tvar(t, "$body")},
				{Key: "ok", Value: &config.Template{Kind: config.TemplateScalar, Raw: "true"}},
			}}},
			{Key: "route", Value: tvar(t, "$route_path")},
		}},
	}}
	resp := ResponseMiddleware(route, nil)(upstream(200, "[1,2]", http.Header{"Content-Type": {"application/json"}}, nil))(newCtx(route, ""))
	if got := string(resp.Body); got != `{"result":{"payload":[1,2],"ok":true},"route":"/v2/admin"}` {
		t.Fatalf("got %s", got)
	}
}

func TestGatewayErrorNotWrapped(t *testing.T) {
	route := responseRoute(t)
	next := func(rc *gateway.RequestContext) *gateway.Response {
		rc.Upstream = &gateway.Response{Status: 503, Header: http.Header{}, Body: []byte(`{"e":1}`), FromUpstream: true}
		return gateway.GatewayTimeout()
	}
	resp := ResponseMiddleware(route, nil)(next)(newCtx(route, ""))
	want := gateway.GatewayTimeout()
	if resp.Status != 504 || string(resp.Body) != string(want.Body) {
		t.Fatalf("got %d %s", resp.Status, resp.Body)
	}
	if !reflect.DeepEqual(resp.Header, want.Header) {
		t.Fatalf("headers %v", resp.Header)
	}
}

func TestUpstream503ThenTimeoutGivesPlain504(t *testing.T) {
	route := responseRoute(t)
	attempts := 0
	next := func(rc *gateway.RequestContext) *gateway.Response {
		var last *gateway.Response
		for attempt := 1; attempt <= 2; attempt++ {
			attempts++
			if attempt == 1 {
				last = &gateway.Response{Status: 503, Header: http.Header{"Server": {"mock"}}, Body: []byte(`{"e":1}`), FromUpstream: true}
				rc.Upstream = last
				rc.ResponseTime = respTime
				continue
			}
			last = gateway.GatewayTimeout()
		}
		return last
	}
	resp := ResponseMiddleware(route, nil)(next)(newCtx(route, ""))
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
	route := responseRoute(t)
	route.RespTransform.Envelope = nil
	var seen http.Header
	resp := ResponseMiddleware(route, nil)(upstream(200, "raw", http.Header{"Server": {"mock"}, "Content-Type": {"text/plain"}}, &seen))(newCtx(route, "gzip"))
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
	route := responseRoute(t)
	next := func(rc *gateway.RequestContext) *gateway.Response {
		return &gateway.Response{Status: 200, Body: []byte(`{"ok": 1}`), FromUpstream: true}
	}
	resp := ResponseMiddleware(route, fakeClock{respTime})(next)(newCtx(route, ""))
	if string(resp.Body) != sampleWant {
		t.Fatalf("got %s", resp.Body)
	}
}

func TestResponsePassThrough(t *testing.T) {
	route := &config.Route{Path: "/x"}
	var seen http.Header
	hdr := http.Header{"Server": {"mock"}}
	resp := ResponseMiddleware(route, nil)(upstream(200, "x", hdr, &seen))(newCtx(route, "gzip"))
	if seen.Get("Accept-Encoding") != "gzip" || string(resp.Body) != "x" || resp.Header.Get("Server") != "mock" {
		t.Fatal("route without transform must pass through")
	}
}
