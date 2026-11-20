package proxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

type seen struct {
	method, uri, host, body string
	header                  http.Header
}

func recorder(t *testing.T, status int, respHeader map[string]string, respBody string) (*httptest.Server, chan seen) {
	ch := make(chan seen, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		ch <- seen{r.Method, r.RequestURI, r.Host, string(b), r.Header.Clone()}
		for k, v := range respHeader {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

func target(t *testing.T, raw string) *contract.Target {
	tg, err := contract.NewTarget(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func reqCtx(method, url, body string, route *contract.Route) *contract.RequestContext {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.RemoteAddr = "10.0.0.5:5555"
	return &contract.RequestContext{Req: r, Route: route, Body: []byte(body)}
}

func TestClientForwardsRequest(t *testing.T) {
	srv, ch := recorder(t, 201, map[string]string{"X-Up": "1", "Content-Type": "text/plain"}, "created")
	route := &contract.Route{Path: "/api/users", Timeout: time.Second}
	ctx := reqCtx("POST", "http://gateway.example:8080/api/users/7?x=1", `{"a":1}`, route)
	h := ctx.Req.Header
	h.Set("X-Custom", "1")
	h.Set("X-Forwarded-For", "1.2.3.4")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("Proxy-Authorization", "x")
	h.Set("Upgrade", "websocket")
	h.Set("Connection", "X-Drop")
	h.Set("X-Drop", "1")
	h.Set("Te", "trailers")
	h.Set("Accept-Encoding", "gzip")

	resp, err := NewClient().Send(context.Background(), target(t, srv.URL), BuildRequest(ctx))
	if err != nil {
		t.Fatal(err)
	}
	got := <-ch
	wantHost := strings.TrimPrefix(srv.URL, "http://")
	if got.method != "POST" || got.uri != "/api/users/7?x=1" || got.body != `{"a":1}` || got.host != wantHost {
		t.Fatalf("upstream saw %+v", got)
	}
	for k, v := range map[string]string{
		"X-Custom":          "1",
		"X-Forwarded-For":   "1.2.3.4, 10.0.0.5",
		"X-Forwarded-Proto": "http",
		"X-Forwarded-Host":  "gateway.example:8080",
		"Accept-Encoding":   "gzip",
	} {
		if g := got.header.Get(k); g != v {
			t.Errorf("header %s = %q want %q", k, g, v)
		}
	}
	for _, k := range []string{"Keep-Alive", "Proxy-Authorization", "Upgrade", "X-Drop", "Te", "Connection"} {
		if _, ok := got.header[k]; ok {
			t.Errorf("hop header %s forwarded", k)
		}
	}
	if resp.Status != 201 || string(resp.Body) != "created" || resp.Header.Get("X-Up") != "1" || !resp.FromUpstream {
		t.Fatalf("response %+v", resp)
	}
}

func TestClientEnvelopeIdentityAndNoAcceptEncoding(t *testing.T) {
	srv, ch := recorder(t, 200, nil, "ok")
	plain := &contract.Route{Path: "/a"}
	ctx := reqCtx("GET", "http://g/a", "", plain)
	if _, err := NewClient().Send(context.Background(), target(t, srv.URL), BuildRequest(ctx)); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.header.Get("Accept-Encoding") != "" {
		t.Fatalf("gateway added Accept-Encoding %q", got.header.Get("Accept-Encoding"))
	}
	env := &contract.Route{Path: "/a", RespTransform: &contract.RespTransform{Envelope: &contract.Template{}}}
	ctx = reqCtx("GET", "http://g/a", "", env)
	ctx.Req.Header.Set("Accept-Encoding", "gzip")
	if _, err := NewClient().Send(context.Background(), target(t, srv.URL), BuildRequest(ctx)); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.header.Get("Accept-Encoding") != "identity" {
		t.Fatalf("envelope route sent Accept-Encoding %q", got.header.Get("Accept-Encoding"))
	}
}

func TestClientBasePathAndStrip(t *testing.T) {
	srv, ch := recorder(t, 200, nil, "")
	route := &contract.Route{Path: "/svc", StripPrefix: true}
	ctx := reqCtx("GET", "http://g/svc/a?q=1", "", route)
	if _, err := NewClient().Send(context.Background(), target(t, srv.URL+"/base"), BuildRequest(ctx)); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.uri != "/base/a?q=1" {
		t.Fatalf("uri %q", got.uri)
	}
}

func TestClientNoRedirectAndGzipUntouched(t *testing.T) {
	srv, _ := recorder(t, 302, map[string]string{"Location": "/elsewhere", "Content-Encoding": "gzip"}, "zz")
	ctx := reqCtx("GET", "http://g/a", "", &contract.Route{Path: "/a"})
	resp, err := NewClient().Send(context.Background(), target(t, srv.URL), BuildRequest(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 302 || resp.Header.Get("Location") != "/elsewhere" || string(resp.Body) != "zz" {
		t.Fatalf("response %+v", resp)
	}
}

func TestAttemptSlowBodyTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
		io.WriteString(w, "late")
	}))
	defer srv.Close()
	ctx := reqCtx("GET", "http://g/a", "", &contract.Route{Path: "/a"})
	resp, err := Attempt(context.Background(), NewClient(), target(t, srv.URL), BuildRequest(ctx), 100*time.Millisecond)
	res, status := Outcome(resp, err)
	if status != 504 || res.Status != 504 || res.FromUpstream {
		t.Fatalf("status %d res %+v err %v", status, res, err)
	}
}

func TestAttemptClosedSocketIs502(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			bufio.NewReader(c).ReadString('\n')
			c.Close()
		}
	}()
	ctx := reqCtx("POST", "http://g/a", "x", &contract.Route{Path: "/a"})
	resp, err := Attempt(context.Background(), NewClient(), target(t, "http://"+ln.Addr().String()), BuildRequest(ctx), time.Second)
	res, status := Outcome(resp, err)
	if status != 502 || res.Status != 502 || res.FromUpstream {
		t.Fatalf("status %d res %+v err %v", status, res, err)
	}
}

func TestAttemptRefusedIs502(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	ctx := reqCtx("GET", "http://g/a", "", &contract.Route{Path: "/a"})
	resp, err := Attempt(context.Background(), NewClient(), target(t, "http://"+addr), BuildRequest(ctx), time.Second)
	if _, status := Outcome(resp, err); status != 502 {
		t.Fatalf("status %d err %v", status, err)
	}
}

func TestAttemptClientCancelNotClassified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	parent, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	ctx := reqCtx("GET", "http://g/a", "", &contract.Route{Path: "/a"})
	resp, err := Attempt(parent, NewClient(), target(t, srv.URL), BuildRequest(ctx), 5*time.Second)
	res, status := Outcome(resp, err)
	if status != contract.StatusClientClosed || contract.Classify(resp, err) != contract.StatusClientClosed || res.FromUpstream {
		t.Fatalf("status %d err %v", status, err)
	}
}

func TestHandlerRoundTrip(t *testing.T) {
	srv, ch := recorder(t, 418, map[string]string{"X-Up": "tea"}, `{"k":7}`)
	route := &contract.Route{Path: "/api", StripPrefix: true, Timeout: time.Second,
		Targets: []*contract.Target{target(t, srv.URL), target(t, "http://127.0.0.1:1")}}
	h := New(route, contract.Deps{})
	ctx := reqCtx("PUT", "http://g/api/x", "body", nil)
	res := h(ctx)
	got := <-ch
	if got.uri != "/x" || got.method != "PUT" || got.body != "body" {
		t.Fatalf("upstream saw %+v", got)
	}
	if res.Status != 418 || string(res.Body) != `{"k":7}` || res.Header.Get("X-Up") != "tea" || !res.FromUpstream {
		t.Fatalf("res %+v", res)
	}
	if ctx.Upstream != res || ctx.ResponseTime.IsZero() {
		t.Fatal("request context not updated")
	}
}

func TestHandlerGatewayErrorNotFromUpstream(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	route := &contract.Route{Path: "/", Timeout: time.Second, Targets: []*contract.Target{target(t, "http://"+addr)}}
	res := New(route, contract.Deps{})(reqCtx("GET", "http://g/", "", nil))
	if res.Status != 502 || res.FromUpstream || !strings.Contains(string(res.Body), `"bad_gateway"`) {
		t.Fatalf("res %+v %s", res, res.Body)
	}
}

func TestBuildRequestBodyFallbackAndTLS(t *testing.T) {
	r := httptest.NewRequest("POST", "https://gw.example/a?z=9", strings.NewReader("raw"))
	r.RemoteAddr = "192.0.2.1:1234"
	ctx := &contract.RequestContext{Req: r, Route: &contract.Route{Path: "/a"}}
	out := BuildRequest(ctx)
	if string(out.Body) != "raw" || string(ctx.Body) != "raw" {
		t.Fatalf("body %q ctx %q", out.Body, ctx.Body)
	}
	if out.Path != "/a?z=9" || out.Method != "POST" {
		t.Fatalf("out %+v", out)
	}
	h := out.Header
	if h.Get("X-Forwarded-For") != "192.0.2.1" || h.Get("X-Forwarded-Proto") != "https" || h.Get("X-Forwarded-Host") != "gw.example" {
		t.Fatalf("headers %v", h)
	}
	if ctx.Req.Header.Get("X-Forwarded-For") != "" {
		t.Fatal("client request headers mutated")
	}
}
