package e2e

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

var xfProductPorts = []int{3003, 3004}

type xfEnvelope struct {
	Data            json.RawMessage `json:"data"`
	GatewayMetadata struct {
		ServedAt string `json:"served_at"`
		Route    string `json:"route"`
	} `json:"gateway_metadata"`
}

// xfSampleEnvelope builds the exact sample envelope body around data.
func xfSampleEnvelope(data, servedAt string) string {
	return `{"data":` + data + `,"gateway_metadata":{"served_at":"` + servedAt + `","route":"/api/legacy"}}`
}

func xfVariantBEnvelope(payload string) string {
	return `{"result":{"payload":` + payload + `,"ok":true},"route":"/v2/admin"}`
}

// xfJSONBody checks Content-Type application/json and a Content-Length matching the body.
func xfJSONBody(t testing.TB, r *Resp) {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); !isJSONType(ct) {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cl := r.Header.Get("Content-Length"); cl != strconv.Itoa(len(r.Body)) {
		t.Errorf("Content-Length = %q, want %d", cl, len(r.Body))
	}
}

// xfSampleHeaders checks the sample /api/legacy response header transform.
func xfSampleHeaders(t testing.TB, r *Resp) {
	t.Helper()
	if got := r.Header.Get("X-Served-By"); got != "gatewaykit" {
		t.Errorf("X-Served-By = %q, want gatewaykit", got)
	}
	for _, h := range []string{"Server", "X-Powered-By"} {
		if v, ok := r.Header[h]; ok {
			t.Errorf("client got %s: %q, want it removed", h, v)
		}
	}
	if got := r.Header.Get("X-Mock-Service"); got != "legacy" {
		t.Errorf("X-Mock-Service = %q, want legacy", got)
	}
}

// xfServedAt decodes the sample envelope and checks served_at against the client's window.
func xfServedAt(t testing.TB, r *Resp) (xfEnvelope, time.Time) {
	t.Helper()
	var env xfEnvelope
	r.JSON(t, &env)
	ts := xfParseTime(t, "served_at", env.GatewayMetadata.ServedAt)
	xfBetween(t, "served_at", ts, r.Start, r.Start.Add(r.Elapsed+time.Second))
	return env, ts
}

// xfNotWrapped fails if a gateway error carries envelope keys or response header transforms.
func xfNotWrapped(t testing.TB, r *Resp) {
	t.Helper()
	var m map[string]json.RawMessage
	r.JSON(t, &m)
	for _, k := range []string{"data", "gateway_metadata", "result", "route"} {
		if _, ok := m[k]; ok {
			t.Errorf("gateway error body has envelope key %q: %s", k, r.Body)
		}
	}
	if v, ok := r.Header["X-Served-By"]; ok {
		t.Errorf("gateway error has X-Served-By %q, want no response transform", v)
	}
}

func TestTransformResponseEnvelope(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, xfLegacyPort)
	c := NewClient(t)
	id := xfID(t)
	const delay = 200 * time.Millisecond

	r := c.Get("/api/legacy/v1/data?q=1", xfIDHeader, id, "Accept-Encoding", "gzip",
		"X-Mock-Delay-Ms", strconv.Itoa(int(delay/time.Millisecond)))
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", r.Status, r.Body)
	}
	xfJSONBody(t, r)
	xfSampleHeaders(t, r)
	if ce := r.Header.Get("Content-Encoding"); ce != "" {
		t.Errorf("Content-Encoding = %q, want none", ce)
	}

	env, served := xfServedAt(t, r)
	if want := xfSampleEnvelope(string(env.Data), env.GatewayMetadata.ServedAt); string(r.Body) != want {
		t.Errorf("body = %s, want %s", r.Body, want)
	}

	var e Echo
	if err := json.Unmarshal(env.Data, &e); err != nil {
		t.Fatalf("data is not the upstream echo: %v; body %s", err, r.Body)
	}
	if e.Service != "legacy" || e.Method != http.MethodGet || e.Path != "/v1/data" || e.Query["q"] != "1" {
		t.Errorf("data = %+v, want legacy GET /v1/data?q=1 echo", e)
	}
	if got := e.Headers["accept-encoding"]; got != "identity" {
		t.Errorf("upstream Accept-Encoding = %q, want identity", got)
	}

	m := xfOneMockReq(t, id, xfLegacyPort)
	if got := m.Headers["accept-encoding"]; got != "identity" {
		t.Errorf("logged upstream Accept-Encoding = %q, want identity", got)
	}
	if served.Before(m.Time.Add(delay).Truncate(time.Millisecond)) {
		t.Errorf("served_at %s is before upstream receipt %s + %v", served.Format(xfTimeLayout),
			m.Time.UTC().Format(xfTimeLayout), delay)
	}
	reqStart := xfParseTime(t, "X-Request-Start", e.Headers["x-request-start"])
	if d := served.Sub(reqStart); d < delay || d > delay+5*time.Second {
		t.Errorf("served_at - request_time = %v, want in [%v, %v]", d, delay, delay+5*time.Second)
	}
}

func TestTransformResponseBodyValues(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, xfLegacyPort)
	c := NewClient(t)

	cases := []struct{ name, upstream, data string }{
		{"json object", `{"id": 7}`, `{"id":7}`},
		{"json array", `[1, 2]`, `[1,2]`},
		{"plain text", `hello`, `"hello"`},
		{"trailing data is text", `{"a":1}x`, `"{\"a\":1}x"`},
		{"empty", ``, `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := xfID(t)
			r := c.Get("/api/legacy/v1/data", xfIDHeader, id, "X-Mock-Body", tc.upstream)
			if r.Status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", r.Status, r.Body)
			}
			if m := xfOneMockReq(t, id, xfLegacyPort); m.Headers["x-mock-body"] != tc.upstream {
				t.Fatalf("upstream X-Mock-Body = %q, want %q", m.Headers["x-mock-body"], tc.upstream)
			}
			xfJSONBody(t, r)
			xfSampleHeaders(t, r)
			env, _ := xfServedAt(t, r)
			if want := xfSampleEnvelope(tc.data, env.GatewayMetadata.ServedAt); string(r.Body) != want {
				t.Errorf("body = %s, want %s", r.Body, want)
			}
		})
	}
}

func TestTransformResponseUpstreamErrorWrapped(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, xfLegacyPort)
	c := NewClient(t)

	cases := []struct {
		name     string
		status   int
		upstream string
		data     string
	}{
		{"503 json", http.StatusServiceUnavailable, `{"e": 1}`, `{"e":1}`},
		{"500 text", http.StatusInternalServerError, `boom`, `"boom"`},
		{"404 json", http.StatusNotFound, `{"missing": true}`, `{"missing":true}`},
		{"400 empty", http.StatusBadRequest, ``, `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := xfID(t)
			r := c.Get("/api/legacy/v1/data", xfIDHeader, id,
				"X-Mock-Status", strconv.Itoa(tc.status), "X-Mock-Body", tc.upstream)
			if r.Status != tc.status {
				t.Fatalf("status = %d, want upstream's %d; body %s", r.Status, tc.status, r.Body)
			}
			xfJSONBody(t, r)
			xfSampleHeaders(t, r)
			env, _ := xfServedAt(t, r)
			if want := xfSampleEnvelope(tc.data, env.GatewayMetadata.ServedAt); string(r.Body) != want {
				t.Errorf("body = %s, want %s", r.Body, want)
			}
			if n := len(xfMockReqs(t, id, xfLegacyPort)); n != 1 {
				t.Errorf("upstream got %d requests, want 1", n)
			}
		})
	}
}

func TestTransformResponseGatewayErrorNotWrapped(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	c := NewClient(t)

	r := c.Get("/api/legacy/v1/data", xfIDHeader, xfID(t), "X-Mock-Drop", "1")
	RequireError(t, r, http.StatusBadGateway, "bad_gateway")
	xfNotWrapped(t, r)
}

// TestTransformResponseGatewayTimeoutNotWrapped waits out the sample's 30s global timeout, so it runs in parallel.
func TestTransformResponseGatewayTimeoutNotWrapped(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	t.Parallel()
	c := NewClient(t)
	const timeout = 30 * time.Second

	r := c.Get("/api/legacy/v1/data", xfIDHeader, xfID(t), "X-Mock-Delay-Ms", "35000")
	RequireError(t, r, http.StatusGatewayTimeout, "gateway_timeout")
	xfNotWrapped(t, r)
	if r.Elapsed < timeout || r.Elapsed > timeout+10*time.Second {
		t.Errorf("elapsed = %v, want in [%v, %v]", r.Elapsed, timeout, timeout+10*time.Second)
	}
}

func TestTransformResponseVariantBNested(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	ResetMocks(t, xfProductPorts...)
	c := NewClient(t)

	cases := []struct {
		name     string
		status   int
		upstream string
		payload  string
	}{
		{"array", http.StatusOK, `[1,2]`, `[1,2]`},
		{"object", http.StatusOK, `{"b": [true, null], "n": 1.50}`, `{"b":[true,null],"n":1.50}`},
		{"plain text", http.StatusOK, `hi`, `"hi"`},
		{"empty", http.StatusOK, ``, `null`},
		{"upstream 503 kept", http.StatusServiceUnavailable, `[1,2]`, `[1,2]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := xfID(t)
			r := c.Get("/v2/admin/x", "Authorization", "k1", xfIDHeader, id,
				"X-Mock-Status", strconv.Itoa(tc.status), "X-Mock-Body", tc.upstream, "Accept-Encoding", "gzip")
			if r.Status != tc.status {
				t.Fatalf("status = %d, want %d; body %s", r.Status, tc.status, r.Body)
			}
			if want := xfVariantBEnvelope(tc.payload); string(r.Body) != want {
				t.Errorf("body = %s, want %s", r.Body, want)
			}
			xfJSONBody(t, r)
			m := xfOneMockReq(t, id, xfProductPorts...)
			if got := m.Headers["accept-encoding"]; got != "identity" {
				t.Errorf("upstream Accept-Encoding = %q, want identity", got)
			}
		})
	}
}

func TestTransformResponseVariantBGatewayErrors(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	ResetMocks(t, xfProductPorts...)
	t.Cleanup(func() { ResetMocks(t, xfProductPorts...) })
	c := NewClient(t)
	const timeout = 750 * time.Millisecond
	const backoff = 100 * time.Millisecond

	t.Run("timeout", func(t *testing.T) {
		id := xfID(t)
		r := c.Get("/v2/admin/x", "Authorization", "k1", xfIDHeader, id, "X-Mock-Delay-Ms", "1500")
		RequireError(t, r, http.StatusGatewayTimeout, "gateway_timeout")
		xfNotWrapped(t, r)
		if r.Elapsed < timeout || r.Elapsed > timeout+5*time.Second {
			t.Errorf("elapsed = %v, want in [%v, %v]", r.Elapsed, timeout, timeout+5*time.Second)
		}
		if n := len(xfMockReqs(t, id, xfProductPorts...)); n != 1 {
			t.Errorf("upstream got %d requests, want 1 (504 is not in retry.on)", n)
		}
	})

	t.Run("upstream 500 then timeout", func(t *testing.T) {
		probe := c.Get("/v2/admin/probe", "Authorization", "k1", xfIDHeader, xfID(t))
		if probe.Status != http.StatusOK {
			t.Fatalf("probe status = %d; body %s", probe.Status, probe.Body)
		}
		var p struct {
			Result struct {
				Payload Echo `json:"payload"`
			} `json:"result"`
		}
		probe.JSON(t, &p)
		last, next := 3003, 3004
		switch p.Result.Payload.Service {
		case "products-a":
		case "products-b":
			last, next = 3004, 3003
		default:
			t.Fatalf("probe served by %q; body %s", p.Result.Payload.Service, probe.Body)
		}
		SetMockDefault(t, next, http.StatusInternalServerError, 0)
		SetMockDefault(t, last, http.StatusOK, 1500)

		id := xfID(t)
		r := c.Get("/v2/admin/x", "Authorization", "k1", xfIDHeader, id)
		RequireError(t, r, http.StatusGatewayTimeout, "gateway_timeout")
		xfNotWrapped(t, r)
		if lo := backoff + timeout; r.Elapsed < lo || r.Elapsed > lo+5*time.Second {
			t.Errorf("elapsed = %v, want in [%v, %v]", r.Elapsed, lo, lo+5*time.Second)
		}
		if n := len(xfMockReqs(t, id, next)); n != 1 {
			t.Errorf("first target %d got %d requests, want 1", next, n)
		}
		if n := len(xfMockReqs(t, id, last)); n != 1 {
			t.Errorf("retry target %d got %d requests, want 1", last, n)
		}
	})
}
