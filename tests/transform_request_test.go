package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	xfLegacyPort = 3005
	xfIDHeader   = "X-E2e-Id"
	xfTimeLayout = "2006-01-02T15:04:05.000Z"
)

var (
	xfTimeRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
	xfSeq    atomic.Int64
)

// xfMockReq is a mock log entry with the body kept as raw (compacted) JSON.
type xfMockReq struct {
	Port    int               `json:"-"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	URI     string            `json:"uri"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	Time    time.Time         `json:"time"`
}

// xfID returns a unique value for xfIDHeader so log lookups ignore other traffic.
func xfID(t testing.TB) string {
	return fmt.Sprintf("%s-%d-%d", t.Name(), time.Now().UnixNano(), xfSeq.Add(1))
}

// xfMockReqs returns the logged requests tagged with id across ports.
func xfMockReqs(t testing.TB, id string, ports ...int) []xfMockReq {
	t.Helper()
	var out []xfMockReq
	for _, p := range ports {
		var all []xfMockReq
		if err := json.Unmarshal(mockAdmin(t, p, http.MethodGet, "/__mock/requests", nil), &all); err != nil {
			t.Fatalf("mock %d requests: %v", p, err)
		}
		for _, r := range all {
			if r.Headers[strings.ToLower(xfIDHeader)] == id {
				r.Port = p
				out = append(out, r)
			}
		}
	}
	return out
}

// xfOneMockReq returns the single logged request tagged with id across ports.
func xfOneMockReq(t testing.TB, id string, ports ...int) xfMockReq {
	t.Helper()
	reqs := xfMockReqs(t, id, ports...)
	if len(reqs) != 1 {
		t.Fatalf("mocks %v logged %d requests tagged %s, want 1", ports, len(reqs), id)
	}
	return reqs[0]
}

// xfParseTime parses a gateway timestamp, failing unless it is ISO-8601 UTC with milliseconds.
func xfParseTime(t testing.TB, what, s string) time.Time {
	t.Helper()
	if !xfTimeRE.MatchString(s) {
		t.Fatalf("%s = %q, want YYYY-MM-DDTHH:MM:SS.mmmZ", what, s)
	}
	ts, err := time.Parse(xfTimeLayout, s)
	if err != nil {
		t.Fatalf("%s = %q: %v", what, s, err)
	}
	return ts
}

// xfBetween fails unless lo (truncated to milliseconds) <= ts <= hi.
func xfBetween(t testing.TB, what string, ts, lo, hi time.Time) {
	t.Helper()
	if ts.Before(lo.Truncate(time.Millisecond)) || ts.After(hi) {
		t.Errorf("%s = %s, want within [%s, %s]", what, ts.Format(xfTimeLayout),
			lo.UTC().Format(xfTimeLayout), hi.UTC().Format(xfTimeLayout))
	}
}

// xfRequestTime checks the upstream's X-Request-Start against the client's send window.
func xfRequestTime(t testing.TB, m xfMockReq, r *Resp) string {
	t.Helper()
	v, ok := m.Headers["x-request-start"]
	if !ok {
		t.Fatalf("upstream got no X-Request-Start; headers %v", m.Headers)
	}
	ts := xfParseTime(t, "X-Request-Start", v)
	xfBetween(t, "X-Request-Start", ts, r.Start, r.Start.Add(r.Elapsed+time.Second))
	return v
}

func xfMapped(user, ts string) string {
	return `{"user":` + user + `,"meta":{"source":"gateway","timestamp":"` + ts + `"}}`
}

func xfLegacyPost(c *Client, id, ctype, body string) *Resp {
	c.T.Helper()
	return c.Req(http.MethodPost, "/api/legacy", body, "Content-Type", ctype, xfIDHeader, id)
}

func TestTransformRequestHeaders(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, xfLegacyPort)
	c := NewClient(t)
	id := xfID(t)

	req, err := http.NewRequest(http.MethodGet, c.Base+"/api/legacy/v1/data?q=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(xfIDHeader, id)
	req.Header["x-debug"] = []string{"1"}
	req.Header["X-Internal"] = []string{"yes"}
	req.Header["X-Gateway"] = []string{"client", "client2"}
	req.Header.Set("X-Keep", "kept")
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", r.Status, r.Body)
	}

	m := xfOneMockReq(t, id, xfLegacyPort)
	if m.Path != "/v1/data" {
		t.Errorf("upstream path = %q, want /v1/data", m.Path)
	}
	if got := m.Headers["x-gateway"]; got != "gatewaykit" {
		t.Errorf("upstream X-Gateway = %q, want only gatewaykit", got)
	}
	for _, h := range []string{"x-debug", "x-internal"} {
		if v, ok := m.Headers[h]; ok {
			t.Errorf("upstream got %s: %q, want it removed", h, v)
		}
	}
	if got := m.Headers["x-keep"]; got != "kept" {
		t.Errorf("upstream X-Keep = %q, want kept", got)
	}
	if string(m.Body) != "null" {
		t.Errorf("upstream body = %s, want empty", m.Body)
	}
	xfRequestTime(t, m, r)
}

func TestTransformRequestBodyMapping(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, xfLegacyPort)
	c := NewClient(t)

	cases := []struct {
		name, ctype, body, user string
	}{
		{"sample", "application/json", `{"userId": 7, "userName": "Ada", "debug": true}`, `{"id":7,"name":"Ada"}`},
		{"missing source", "application/json", `{"userId": 7}`, `{"id":7}`},
		{"null source", "application/json", `{"userId": null}`, `{"id":null}`},
		{"suffix json", "application/vnd.api+json", `{"userId": 7}`, `{"id":7}`},
		{"unmapped dropped", "application/json", `{"userName": "Ada", "extra": {"x": 1}, "user": {"id": 1}}`, `{"name":"Ada"}`},
		{"number fidelity", "application/json", `{"userId": 12345678901234567890}`, `{"id":12345678901234567890}`},
		{"structured value", "application/json", `{"userId": {"a": [1, 2.50]}}`, `{"id":{"a":[1,2.50]}}`},
		{"no html escaping", "application/json", `{"userName": "<b>&</b>"}`, `{"name":"<b>&</b>"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := xfID(t)
			r := xfLegacyPost(c, id, tc.ctype, tc.body)
			if r.Status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", r.Status, r.Body)
			}
			m := xfOneMockReq(t, id, xfLegacyPort)
			ts := xfRequestTime(t, m, r)
			want := xfMapped(tc.user, ts)
			if string(m.Body) != want {
				t.Errorf("upstream body = %s, want %s", m.Body, want)
			}
			if got := m.Headers["content-length"]; got != strconv.Itoa(len(want)) {
				t.Errorf("upstream Content-Length = %q, want %d", got, len(want))
			}
			if got := m.Headers["x-gateway"]; got != "gatewaykit" {
				t.Errorf("upstream X-Gateway = %q, want gatewaykit", got)
			}
		})
	}
}

func TestTransformRequestBodyPassthrough(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, xfLegacyPort)
	c := NewClient(t)

	cases := []struct {
		name, ctype, body string
		logged            string // body as the mock logs it (compacted JSON or a JSON string)
		length            []string
	}{
		{"form body", "application/x-www-form-urlencoded", "a=1", `"a=1"`, []string{"3"}},
		{"json array", "application/json", "[1, 2]", "[1,2]", []string{"6"}},
		{"empty json body", "application/json", "", "null", []string{"", "0"}},
		{"json text with non-json type", "text/plain", `{"userId": 7}`, `{"userId":7}`, []string{"13"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := xfID(t)
			r := xfLegacyPost(c, id, tc.ctype, tc.body)
			if r.Status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", r.Status, r.Body)
			}
			m := xfOneMockReq(t, id, xfLegacyPort)
			if string(m.Body) != tc.logged {
				t.Errorf("upstream body = %s, want %s unchanged", m.Body, tc.logged)
			}
			cl := m.Headers["content-length"]
			ok := false
			for _, l := range tc.length {
				ok = ok || cl == l
			}
			if !ok {
				t.Errorf("upstream Content-Length = %q, want one of %q", cl, tc.length)
			}
			if got := m.Headers["content-type"]; got != tc.ctype {
				t.Errorf("upstream Content-Type = %q, want %q", got, tc.ctype)
			}
			xfRequestTime(t, m, r)
		})
	}
}

func TestTransformRequestInvalidJSON(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, xfLegacyPort)
	c := NewClient(t)

	cases := []struct{ name, ctype, body string }{
		{"truncated", "application/json", `{"userId":`},
		{"trailing data", "application/json", `{"userId": 7}x`},
		{"two values", "application/json", `{"userId": 7} {"userId": 8}`},
		{"suffix json", "application/vnd.api+json", `{"userId"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := xfID(t)
			r := xfLegacyPost(c, id, tc.ctype, tc.body)
			RequireError(t, r, http.StatusBadRequest, "invalid_json_body")
			xfNotWrapped(t, r)
			if n := len(xfMockReqs(t, id, xfLegacyPort)); n != 0 {
				t.Errorf("upstream got %d requests, want 0", n)
			}
		})
	}
}
