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

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
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

func target(t *testing.T, raw string) *config.Target {
	tg, err := config.NewTarget(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func reqCtx(method, url, body string, route *config.Route) *gateway.RequestContext {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.RemoteAddr = "10.0.0.5:5555"
	return &gateway.RequestContext{Req: r, Route: route, Body: []byte(body)}
}

func TestClientForwardsRequest(t *testing.T) {
	srv, ch := recorder(t, 201, map[string]string{"X-Up": "1", "Content-Type": "text/plain"}, "created")
	route := &config.Route{Path: "/api/users", Timeout: time.Second}
	rc := reqCtx("POST", "http://gateway.example:8080/api/users/7?x=1", `{"a":1}`, route)
	h := rc.Req.Header
	h.Set("X-Custom", "1")
	h.Set("X-Forwarded-For", "1.2.3.4")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("Proxy-Authorization", "x")
	h.Set("Upgrade", "websocket")
	h.Set("Connection", "X-Drop")
	h.Set("X-Drop", "1")
	h.Set("Te", "trailers")
	h.Set("Accept-Encoding", "gzip")

	resp, err := NewClient().Send(context.Background(), target(t, srv.URL), BuildRequest(rc))
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
	plain := &config.Route{Path: "/a"}
	rc := reqCtx("GET", "http://g/a", "", plain)
	if _, err := NewClient().Send(context.Background(), target(t, srv.URL), BuildRequest(rc)); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.header.Get("Accept-Encoding") != "" {
		t.Fatalf("gateway added Accept-Encoding %q", got.header.Get("Accept-Encoding"))
	}
	env := &config.Route{Path: "/a", RespTransform: &config.RespTransform{Envelope: &config.Template{}}}
	rc = reqCtx("GET", "http://g/a", "", env)
	rc.Req.Header.Set("Accept-Encoding", "gzip")
	if _, err := NewClient().Send(context.Background(), target(t, srv.URL), BuildRequest(rc)); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.header.Get("Accept-Encoding") != "identity" {
		t.Fatalf("envelope route sent Accept-Encoding %q", got.header.Get("Accept-Encoding"))
	}
}

func TestClientBasePathAndStrip(t *testing.T) {
	srv, ch := recorder(t, 200, nil, "")
	route := &config.Route{Path: "/svc", StripPrefix: true}
	rc := reqCtx("GET", "http://g/svc/a?q=1", "", route)
	if _, err := NewClient().Send(context.Background(), target(t, srv.URL+"/base"), BuildRequest(rc)); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.uri != "/base/a?q=1" {
		t.Fatalf("uri %q", got.uri)
	}
}

func TestClientNoRedirectAndGzipUntouched(t *testing.T) {
	srv, _ := recorder(t, 302, map[string]string{"Location": "/elsewhere", "Content-Encoding": "gzip"}, "zz")
	rc := reqCtx("GET", "http://g/a", "", &config.Route{Path: "/a"})
	resp, err := NewClient().Send(context.Background(), target(t, srv.URL), BuildRequest(rc))
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
	rc := reqCtx("GET", "http://g/a", "", &config.Route{Path: "/a"})
	resp, err := Attempt(context.Background(), NewClient(), target(t, srv.URL), BuildRequest(rc), 100*time.Millisecond)
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
	rc := reqCtx("POST", "http://g/a", "x", &config.Route{Path: "/a"})
	resp, err := Attempt(context.Background(), NewClient(), target(t, "http://"+ln.Addr().String()), BuildRequest(rc), time.Second)
	res, status := Outcome(resp, err)
	if status != 502 || res.Status != 502 || res.FromUpstream {
		t.Fatalf("status %d res %+v err %v", status, res, err)
	}
}

func TestAttemptRefusedIs502(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	rc := reqCtx("GET", "http://g/a", "", &config.Route{Path: "/a"})
	resp, err := Attempt(context.Background(), NewClient(), target(t, "http://"+addr), BuildRequest(rc), time.Second)
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
	rc := reqCtx("GET", "http://g/a", "", &config.Route{Path: "/a"})
	resp, err := Attempt(parent, NewClient(), target(t, srv.URL), BuildRequest(rc), 5*time.Second)
	res, status := Outcome(resp, err)
	if status != gateway.StatusClientClosed || Classify(resp, err) != gateway.StatusClientClosed || res.FromUpstream {
		t.Fatalf("status %d err %v", status, err)
	}
}

func TestBuildRequestBodyFallbackAndTLS(t *testing.T) {
	r := httptest.NewRequest("POST", "https://gw.example/a?z=9", strings.NewReader("raw"))
	r.RemoteAddr = "192.0.2.1:1234"
	rc := &gateway.RequestContext{Req: r, Route: &config.Route{Path: "/a"}}
	out := BuildRequest(rc)
	if string(out.Body) != "raw" || string(rc.Body) != "raw" {
		t.Fatalf("body %q ctx %q", out.Body, rc.Body)
	}
	if out.Path != "/a?z=9" || out.Method != "POST" {
		t.Fatalf("out %+v", out)
	}
	h := out.Header
	if h.Get("X-Forwarded-For") != "192.0.2.1" || h.Get("X-Forwarded-Proto") != "https" || h.Get("X-Forwarded-Host") != "gw.example" {
		t.Fatalf("headers %v", h)
	}
	if rc.Req.Header.Get("X-Forwarded-For") != "" {
		t.Fatal("client request headers mutated")
	}
}
