package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, env map[string]string) *httptest.Server {
	t.Helper()
	cfg := loadConfig(func(k string) string { return env[k] })
	srv := httptest.NewServer(newMock(cfg))
	t.Cleanup(srv.Close)
	return srv
}

type call struct {
	method  string
	path    string
	body    string
	headers map[string]string
}

func do(t *testing.T, srv *httptest.Server, c call) (*http.Response, []byte) {
	t.Helper()
	if c.method == "" {
		c.method = http.MethodGet
	}
	req, err := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader(c.body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", c.method, c.path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, b
}

func status(t *testing.T, srv *httptest.Server, c call) int {
	t.Helper()
	resp, _ := do(t, srv, c)
	return resp.StatusCode
}

func admin(t *testing.T, srv *httptest.Server, path, body string) {
	t.Helper()
	if got := status(t, srv, call{method: http.MethodPost, path: path, body: body}); got != http.StatusOK {
		t.Fatalf("POST %s %s: status %d", path, body, got)
	}
}

func requests(t *testing.T, srv *httptest.Server) []logEntry {
	t.Helper()
	resp, b := do(t, srv, call{path: "/__mock/requests"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /__mock/requests: status %d", resp.StatusCode)
	}
	var entries []logEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("decode log %s: %v", b, err)
	}
	return entries
}

func TestLoadConfigDefaults(t *testing.T) {
	c := loadConfig(func(string) string { return "" })
	if c.Service != "mock" || c.Port != "3000" || c.HealthPath != "/healthz" || len(c.ExtraHeaders) != 0 {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestLoadConfigCustom(t *testing.T) {
	env := map[string]string{
		"SERVICE_NAME":           "orders",
		"PORT":                   "3002",
		"HEALTH_PATH":            "/status",
		"EXTRA_RESPONSE_HEADERS": " Server=mock ; X-Powered-By=mock;;bad;X-Eq=a=b",
	}
	c := loadConfig(func(k string) string { return env[k] })
	if c.Service != "orders" || c.Port != "3002" || c.HealthPath != "/status" {
		t.Fatalf("custom: %+v", c)
	}
	want := [][2]string{{"Server", "mock"}, {"X-Powered-By", "mock"}, {"X-Eq", "a=b"}}
	if len(c.ExtraHeaders) != len(want) {
		t.Fatalf("extra headers: %v", c.ExtraHeaders)
	}
	for i := range want {
		if c.ExtraHeaders[i] != want[i] {
			t.Fatalf("extra header %d: got %v want %v", i, c.ExtraHeaders[i], want[i])
		}
	}
}

func TestDefaultsServe(t *testing.T) {
	srv := newTestServer(t, nil)
	resp, b := do(t, srv, call{path: "/anything"})
	if resp.StatusCode != 200 || resp.Header.Get("X-Mock-Service") != "mock" {
		t.Fatalf("status %d service %q", resp.StatusCode, resp.Header.Get("X-Mock-Service"))
	}
	var e echo
	if err := json.Unmarshal(b, &e); err != nil || e.Service != "mock" {
		t.Fatalf("echo %s: %v", b, err)
	}
	if got := status(t, srv, call{path: "/healthz"}); got != 200 {
		t.Fatalf("healthz: %d", got)
	}
}

func TestEchoPost(t *testing.T) {
	srv := newTestServer(t, map[string]string{"SERVICE_NAME": "legacy"})
	resp, b := do(t, srv, call{
		method:  http.MethodPost,
		path:    "/v1/data?x=1",
		body:    `{"user":{"id":7}}`,
		headers: map[string]string{"X-Gateway": "gatewaykit", "Content-Type": "application/json"},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Mock-Service"); got != "legacy" {
		t.Fatalf("X-Mock-Service %q", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type %q", got)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"service", "method", "path", "query", "headers", "body"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("missing %q in %s", k, b)
		}
	}
	if len(raw) != 6 {
		t.Fatalf("unexpected fields in %s", b)
	}
	checks := map[string]string{
		"service": `"legacy"`,
		"method":  `"POST"`,
		"path":    `"/v1/data"`,
		"query":   `{"x":"1"}`,
		"body":    `{"user":{"id":7}}`,
	}
	for k, want := range checks {
		if string(raw[k]) != want {
			t.Fatalf("%s: got %s want %s", k, raw[k], want)
		}
	}
	var headers map[string]string
	if err := json.Unmarshal(raw["headers"], &headers); err != nil {
		t.Fatal(err)
	}
	if headers["x-gateway"] != "gatewaykit" || headers["content-type"] != "application/json" || headers["host"] == "" {
		t.Fatalf("headers %v", headers)
	}
}

func TestEchoBodies(t *testing.T) {
	srv := newTestServer(t, nil)
	cases := []struct{ in, want string }{
		{"", `null`},
		{"plain text", `"plain text"`},
		{`[1,2]`, `[1,2]`},
		{`"s"`, `"s"`},
		{`<a>&`, `"<a>&"`},
	}
	for _, c := range cases {
		_, b := do(t, srv, call{method: http.MethodPut, path: "/b", body: c.in})
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatal(err)
		}
		if string(raw["body"]) != c.want {
			t.Fatalf("body %q: got %s want %s", c.in, raw["body"], c.want)
		}
	}
}

func TestAnyMethod(t *testing.T) {
	srv := newTestServer(t, nil)
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"} {
		if got := status(t, srv, call{method: m, path: "/x/y"}); got != 200 {
			t.Fatalf("%s: %d", m, got)
		}
	}
}

func TestExtraResponseHeaders(t *testing.T) {
	srv := newTestServer(t, map[string]string{"EXTRA_RESPONSE_HEADERS": "Server=mock;X-Powered-By=mock"})
	for _, path := range []string{"/x", "/healthz", "/__mock/requests"} {
		resp, _ := do(t, srv, call{path: path})
		if resp.Header.Get("Server") != "mock" || resp.Header.Get("X-Powered-By") != "mock" {
			t.Fatalf("%s headers: %v", path, resp.Header)
		}
	}
	_, b := do(t, srv, call{path: "/x", headers: map[string]string{"X-Mock-Body": "hi"}})
	if string(b) != "hi" {
		t.Fatalf("body %q", b)
	}
}

func TestNoServerHeaderByDefault(t *testing.T) {
	srv := newTestServer(t, nil)
	resp, _ := do(t, srv, call{path: "/x"})
	if resp.Header.Get("Server") != "" || resp.Header.Get("X-Powered-By") != "" {
		t.Fatalf("unexpected headers: %v", resp.Header)
	}
}

func TestStatusControl(t *testing.T) {
	srv := newTestServer(t, nil)
	for _, c := range []struct {
		header string
		want   int
	}{{"503", 503}, {"404", 404}, {"201", 201}, {"204", 204}, {"abc", 200}, {"99", 200}, {"600", 200}} {
		if got := status(t, srv, call{path: "/s", headers: map[string]string{"X-Mock-Status": c.header}}); got != c.want {
			t.Fatalf("X-Mock-Status %s: got %d want %d", c.header, got, c.want)
		}
	}
}

func TestDelayControl(t *testing.T) {
	srv := newTestServer(t, nil)
	start := time.Now()
	if got := status(t, srv, call{path: "/slow", headers: map[string]string{"X-Mock-Delay-Ms": "200"}}); got != 200 {
		t.Fatalf("status %d", got)
	}
	if el := time.Since(start); el < 200*time.Millisecond {
		t.Fatalf("responded after %v", el)
	}
}

func TestDelayLoggedAtReceipt(t *testing.T) {
	srv := newTestServer(t, nil)
	before := time.Now()
	status(t, srv, call{path: "/slow", headers: map[string]string{"X-Mock-Delay-Ms": "200"}})
	entries := requests(t, srv)
	if len(entries) != 1 {
		t.Fatalf("entries %d", len(entries))
	}
	if d := entries[0].Time.Sub(before); d > 100*time.Millisecond {
		t.Fatalf("logged %v after send, want receipt time", d)
	}
}

func TestFailTimes(t *testing.T) {
	srv := newTestServer(t, nil)
	h := map[string]string{"X-Mock-Fail-Times": "2", "X-Mock-Key": "abc"}
	var got []int
	for range 3 {
		got = append(got, status(t, srv, call{path: "/f", headers: h}))
	}
	if got[0] != 503 || got[1] != 503 || got[2] != 200 {
		t.Fatalf("got %v", got)
	}
}

func TestFailTimesCustomStatus(t *testing.T) {
	srv := newTestServer(t, nil)
	h := map[string]string{"X-Mock-Fail-Times": "1", "X-Mock-Key": "k", "X-Mock-Status": "500"}
	if got := status(t, srv, call{path: "/f", headers: h}); got != 500 {
		t.Fatalf("first %d", got)
	}
	if got := status(t, srv, call{path: "/f", headers: h}); got != 200 {
		t.Fatalf("second %d", got)
	}
}

func TestFailTimesKeysSeparate(t *testing.T) {
	srv := newTestServer(t, nil)
	for _, key := range []string{"abc", "xyz"} {
		h := map[string]string{"X-Mock-Fail-Times": "1", "X-Mock-Key": key}
		if got := status(t, srv, call{path: "/f", headers: h}); got != 503 {
			t.Fatalf("key %s: %d", key, got)
		}
	}
}

func TestFailTimesClearedByReset(t *testing.T) {
	srv := newTestServer(t, nil)
	h := map[string]string{"X-Mock-Fail-Times": "1", "X-Mock-Key": "abc"}
	status(t, srv, call{path: "/f", headers: h})
	admin(t, srv, "/__mock/reset", "")
	if got := status(t, srv, call{path: "/f", headers: h}); got != 503 {
		t.Fatalf("after reset: %d", got)
	}
}

func TestBodyControl(t *testing.T) {
	srv := newTestServer(t, nil)
	resp, b := do(t, srv, call{path: "/t", headers: map[string]string{"X-Mock-Body": "hello", "X-Mock-Status": "502"}})
	if string(b) != "hello" {
		t.Fatalf("body %q", b)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content type %q", ct)
	}
	if resp.StatusCode != 502 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestDropControl(t *testing.T) {
	srv := newTestServer(t, nil)
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/d", strings.NewReader("x"))
	req.Header.Set("X-Mock-Drop", "1")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("want connection error, got status %d", resp.StatusCode)
	}
	if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "EOF") && !strings.Contains(err.Error(), "reset") {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(requests(t, srv)); n != 1 {
		t.Fatalf("dropped request logged %d times", n)
	}
}

func TestRequestLog(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/reset", "")
	if got := requests(t, srv); len(got) != 0 {
		t.Fatalf("after reset: %v", got)
	}
	start := time.Now().Add(-time.Second)
	status(t, srv, call{method: "POST", path: "/a?q=1", body: `{"k":"v"}`, headers: map[string]string{"X-Test": "1"}})
	status(t, srv, call{method: "GET", path: "/b"})
	status(t, srv, call{method: "DELETE", path: "/c", headers: map[string]string{"X-Mock-Status": "500"}})
	entries := requests(t, srv)
	if len(entries) != 3 {
		t.Fatalf("entries %d", len(entries))
	}
	e := entries[0]
	if e.Method != "POST" || e.Path != "/a" || e.URI != "/a?q=1" || e.Query["q"] != "1" || e.Headers["x-test"] != "1" {
		t.Fatalf("entry %+v", e)
	}
	if b, _ := json.Marshal(e.Body); string(b) != `{"k":"v"}` {
		t.Fatalf("body %s", b)
	}
	if e.Time.Before(start) || e.Time.After(time.Now()) {
		t.Fatalf("time %v", e.Time)
	}
	if entries[1].Path != "/b" || entries[2].Method != "DELETE" {
		t.Fatalf("order %+v", entries)
	}
}

func TestRequestLogEmptyIsArray(t *testing.T) {
	srv := newTestServer(t, nil)
	_, b := do(t, srv, call{path: "/__mock/requests"})
	if strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("empty log %q", b)
	}
}

func TestAdminCallsNotLogged(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/reset", "")
	admin(t, srv, "/__mock/health", `{"status":200}`)
	admin(t, srv, "/__mock/default", `{"status":200,"delay_ms":0}`)
	status(t, srv, call{path: "/__mock/unknown"})
	status(t, srv, call{path: "/__mock"})
	if got := requests(t, srv); len(got) != 0 {
		t.Fatalf("log %v", got)
	}
}

func TestHealthNotLogged(t *testing.T) {
	srv := newTestServer(t, nil)
	for range 3 {
		status(t, srv, call{path: "/healthz"})
	}
	status(t, srv, call{path: "/proxied"})
	entries := requests(t, srv)
	if len(entries) != 1 || entries[0].Path != "/proxied" {
		t.Fatalf("log %+v", entries)
	}
}

func TestCustomHealthPathNotLogged(t *testing.T) {
	srv := newTestServer(t, map[string]string{"HEALTH_PATH": "/status"})
	status(t, srv, call{path: "/status"})
	status(t, srv, call{path: "/healthz"})
	entries := requests(t, srv)
	if len(entries) != 1 || entries[0].Path != "/healthz" {
		t.Fatalf("log %+v", entries)
	}
}

func TestHealthOverride(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/health", `{"status":503}`)
	if got := status(t, srv, call{path: "/healthz"}); got != 503 {
		t.Fatalf("after override: %d", got)
	}
	if got := status(t, srv, call{path: "/other"}); got != 200 {
		t.Fatalf("non-health path: %d", got)
	}
	admin(t, srv, "/__mock/health", `{"status":200}`)
	if got := status(t, srv, call{path: "/healthz"}); got != 200 {
		t.Fatalf("recovered: %d", got)
	}
	admin(t, srv, "/__mock/health", `{"status":500}`)
	admin(t, srv, "/__mock/reset", "")
	if got := status(t, srv, call{path: "/healthz"}); got != 200 {
		t.Fatalf("after reset: %d", got)
	}
}

func TestHealthIgnoresDefaultOverride(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/default", `{"status":503,"delay_ms":0}`)
	if got := status(t, srv, call{path: "/healthz"}); got != 200 {
		t.Fatalf("healthz with default override: %d", got)
	}
	if got := requests(t, srv); len(got) != 0 {
		t.Fatalf("health probe logged: %+v", got)
	}
	if got := status(t, srv, call{path: "/api"}); got != 503 {
		t.Fatalf("proxied request: %d", got)
	}
}

func TestHealthIgnoresDefaultDelay(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/default", `{"status":200,"delay_ms":5000}`)
	start := time.Now()
	status(t, srv, call{path: "/healthz"})
	if el := time.Since(start); el > time.Second {
		t.Fatalf("health delayed %v", el)
	}
}

func TestHealthIgnoresControlHeaders(t *testing.T) {
	srv := newTestServer(t, nil)
	h := map[string]string{
		"X-Mock-Status":     "500",
		"X-Mock-Body":       "nope",
		"X-Mock-Fail-Times": "5",
		"X-Mock-Key":        "h",
		"X-Mock-Drop":       "1",
		"X-Mock-Delay-Ms":   "5000",
	}
	start := time.Now()
	resp, b := do(t, srv, call{path: "/healthz", headers: h})
	if resp.StatusCode != 200 || string(b) == "nope" || time.Since(start) > time.Second {
		t.Fatalf("health with control headers: %d %q", resp.StatusCode, b)
	}
	admin(t, srv, "/__mock/health", `{"status":503}`)
	if got := status(t, srv, call{path: "/healthz", headers: map[string]string{"X-Mock-Status": "200"}}); got != 503 {
		t.Fatalf("health override with X-Mock-Status: %d", got)
	}
}

func TestDefaultOverride(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/default", `{"status":503,"delay_ms":0}`)
	for _, p := range []string{"/a", "/b", "/c?x=1"} {
		if got := status(t, srv, call{method: "POST", path: p}); got != 503 {
			t.Fatalf("%s: %d", p, got)
		}
	}
	admin(t, srv, "/__mock/reset", "")
	if got := status(t, srv, call{path: "/a"}); got != 200 {
		t.Fatalf("after reset: %d", got)
	}
}

func TestDefaultOverrideDelay(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/default", `{"status":500,"delay_ms":150}`)
	start := time.Now()
	if got := status(t, srv, call{path: "/a"}); got != 500 {
		t.Fatalf("status %d", got)
	}
	if el := time.Since(start); el < 150*time.Millisecond {
		t.Fatalf("responded after %v", el)
	}
}

func TestDefaultOverrideStatusOptional(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/default", `{"delay_ms":1}`)
	if got := status(t, srv, call{path: "/a"}); got != 200 {
		t.Fatalf("status %d", got)
	}
}

func TestHeadersOverrideDefault(t *testing.T) {
	srv := newTestServer(t, nil)
	admin(t, srv, "/__mock/default", `{"status":503}`)
	if got := status(t, srv, call{path: "/a", headers: map[string]string{"X-Mock-Status": "201"}}); got != 201 {
		t.Fatalf("X-Mock-Status over default: %d", got)
	}
}

func TestAdminValidation(t *testing.T) {
	srv := newTestServer(t, nil)
	bad := []call{
		{method: "POST", path: "/__mock/health", body: `{"status":"x"}`},
		{method: "POST", path: "/__mock/health", body: `{}`},
		{method: "POST", path: "/__mock/health", body: `{"status":42}`},
		{method: "POST", path: "/__mock/default", body: `not json`},
		{method: "POST", path: "/__mock/default", body: `{"status":503,"delay_ms":-1}`},
		{method: "POST", path: "/__mock/default", body: `{"stat":503}`},
	}
	for _, c := range bad {
		if got := status(t, srv, c); got != 400 {
			t.Fatalf("%s %s: %d", c.path, c.body, got)
		}
	}
	if got := status(t, srv, call{method: "GET", path: "/__mock/reset"}); got != 405 {
		t.Fatalf("GET reset: %d", got)
	}
	if got := status(t, srv, call{method: "POST", path: "/__mock/requests"}); got != 405 {
		t.Fatalf("POST requests: %d", got)
	}
	if got := status(t, srv, call{path: "/__mock/nope"}); got != 404 {
		t.Fatalf("unknown admin: %d", got)
	}
	if got := status(t, srv, call{path: "/x"}); got != 200 {
		t.Fatalf("state changed by bad admin calls: %d", got)
	}
}

func TestResetClearsEverything(t *testing.T) {
	srv := newTestServer(t, nil)
	status(t, srv, call{path: "/a"})
	admin(t, srv, "/__mock/health", `{"status":503}`)
	admin(t, srv, "/__mock/default", `{"status":503}`)
	admin(t, srv, "/__mock/reset", "")
	if got := requests(t, srv); len(got) != 0 {
		t.Fatalf("log after reset: %v", got)
	}
	if status(t, srv, call{path: "/healthz"}) != 200 || status(t, srv, call{path: "/a"}) != 200 {
		t.Fatal("overrides survived reset")
	}
}

func TestConcurrentRequests(t *testing.T) {
	srv := newTestServer(t, nil)
	done := make(chan struct{})
	for range 20 {
		go func() {
			defer func() { done <- struct{}{} }()
			resp, err := srv.Client().Post(srv.URL+"/c", "application/json", bytes.NewReader([]byte(`{}`)))
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	for range 20 {
		<-done
	}
	if n := len(requests(t, srv)); n != 20 {
		t.Fatalf("logged %d", n)
	}
}
