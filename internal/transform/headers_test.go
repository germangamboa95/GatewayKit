package transform

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

var reqTime = time.Date(2026, 11, 19, 19, 43, 5, 123_000_000, time.UTC)

func mustVar(t *testing.T, s string) config.Var {
	t.Helper()
	v, err := config.ParseVar(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestResolve(t *testing.T) {
	env := Env{
		RequestTime:  reqTime,
		ResponseTime: time.Date(2026, 11, 19, 19, 43, 6, 0, time.UTC),
		RoutePath:    "/api/legacy",
	}
	cases := map[string]string{
		"$request_time":    "2026-11-19T19:43:05.123Z",
		"$response_time":   "2026-11-19T19:43:06.000Z",
		"$route_path":      "/api/legacy",
		"$literal:gateway": "gateway",
		"$literal:$foo":    "$foo",
		"v=$request_time":  "v=$request_time",
		"plain":            "plain",
		"$literal:":        "",
	}
	for in, want := range cases {
		v, err := config.ParseVar(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := Resolve(v, env); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveNonUTC(t *testing.T) {
	loc := time.FixedZone("x", -5*3600)
	got := Resolve(config.Var{Kind: config.VarRequestTime}, Env{RequestTime: reqTime.In(loc)})
	if got != "2026-11-19T19:43:05.123Z" {
		t.Fatalf("got %q", got)
	}
}

func sampleAdds(t *testing.T) []config.HeaderAdd {
	return []config.HeaderAdd{
		{Name: "x-gateway", Value: mustVar(t, "gatewaykit")},
		{Name: "x-request-start", Value: mustVar(t, "$request_time")},
	}
}

func TestApplyHeadersSample(t *testing.T) {
	h := http.Header{}
	h.Set("X-Gateway", "client")
	h.Set("x-debug", "1")
	h.Set("X-Internal", "yes")
	h.Set("Accept", "*/*")
	ApplyHeaders(h, []string{"x-debug", "x-internal"}, sampleAdds(t), Env{RequestTime: reqTime})
	want := http.Header{
		"X-Gateway":       {"gatewaykit"},
		"X-Request-Start": {"2026-11-19T19:43:05.123Z"},
		"Accept":          {"*/*"},
	}
	if !reflect.DeepEqual(h, want) {
		t.Fatalf("got %v, want %v", h, want)
	}
}

func TestApplyHeadersAbsentRemove(t *testing.T) {
	h := http.Header{}
	ApplyHeaders(h, []string{"X-Debug"}, nil, Env{})
	if len(h) != 0 {
		t.Fatalf("got %v", h)
	}
}

func TestApplyHeadersRemoveThenAdd(t *testing.T) {
	h := http.Header{"X-Gateway": {"client", "second"}}
	ApplyHeaders(h, []string{"X-GATEWAY"}, []config.HeaderAdd{{Name: "x-gateway", Value: config.Literal("gatewaykit")}}, Env{})
	if got := h.Values("X-Gateway"); !reflect.DeepEqual(got, []string{"gatewaykit"}) {
		t.Fatalf("got %v", got)
	}
}

func TestApplyHeadersResponse(t *testing.T) {
	h := http.Header{"Server": {"mock"}, "X-Powered-By": {"mock"}, "Content-Type": {"text/plain"}}
	ApplyHeaders(h, []string{"server", "x-powered-by"},
		[]config.HeaderAdd{{Name: "x-served-by", Value: config.Literal("gatewaykit")}}, Env{})
	want := http.Header{"Content-Type": {"text/plain"}, "X-Served-By": {"gatewaykit"}}
	if !reflect.DeepEqual(h, want) {
		t.Fatalf("got %v", h)
	}
}

func TestApplyHeadersManagedIgnored(t *testing.T) {
	h := http.Header{
		"Host":           {"target.example"},
		"Content-Length": {"10"},
		"Connection":     {"keep-alive"},
	}
	add := []config.HeaderAdd{
		{Name: "host", Value: config.Literal("evil.example")},
		{Name: "content-length", Value: config.Literal("1")},
		{Name: "transfer-encoding", Value: config.Literal("chunked")},
		{Name: "proxy-authorization", Value: config.Literal("x")},
		{Name: "upgrade", Value: config.Literal("h2c")},
	}
	ApplyHeaders(h, []string{"Host", "Content-Length", "Connection"}, add, Env{})
	want := http.Header{
		"Host":           {"target.example"},
		"Content-Length": {"10"},
		"Connection":     {"keep-alive"},
	}
	if !reflect.DeepEqual(h, want) {
		t.Fatalf("got %v", h)
	}
}

func TestIsManaged(t *testing.T) {
	for _, n := range []string{"Host", "content-length", "Transfer-Encoding", "Connection", "Keep-Alive", "TE", "Trailer", "Upgrade", "Proxy-Connection", "proxy-authenticate"} {
		if !IsManaged(n) {
			t.Errorf("%s should be managed", n)
		}
	}
	for _, n := range []string{"X-Gateway", "Accept-Encoding", "Server", "Content-Type"} {
		if IsManaged(n) {
			t.Errorf("%s should not be managed", n)
		}
	}
}
