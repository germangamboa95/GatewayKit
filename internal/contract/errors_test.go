package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
)

func TestErrorBodies(t *testing.T) {
	cases := []struct {
		name   string
		resp   *Response
		status int
		body   string
		header map[string]string
	}{
		{"invalid json", InvalidJSONBody(), 400,
			`{"error":"invalid_json_body","message":"request body is not valid JSON"}`, nil},
		{"missing key", Unauthorized(ErrMissingAPIKey), 401,
			`{"error":"unauthorized","message":"missing API key"}`, nil},
		{"invalid key", Unauthorized(ErrInvalidAPIKey), 401,
			`{"error":"unauthorized","message":"invalid API key"}`, nil},
		{"not found", NotFound(), 404,
			`{"error":"not_found","message":"no route matches the request path"}`, nil},
		{"method not allowed", MethodNotAllowed([]string{"GET", "DELETE"}), 405,
			`{"error":"method_not_allowed","message":"method not allowed on this route"}`,
			map[string]string{"Allow": "GET, DELETE"}},
		{"rate limited", RateLimited(7), 429,
			`{"error":"rate_limit_exceeded","message":"rate limit exceeded","retry_after":7}`,
			map[string]string{"Retry-After": "7"}},
		{"bad gateway", BadGateway(), 502,
			`{"error":"bad_gateway","message":"upstream unreachable or sent an invalid response"}`, nil},
		{"breaker open", BreakerOpen(12), 503,
			`{"error":"service_unavailable","retry_after":12}`,
			map[string]string{"Retry-After": "12"}},
		{"no healthy upstream", NoHealthyUpstream(), 503,
			`{"error":"no_healthy_upstream","message":"no healthy upstream target"}`, nil},
		{"gateway timeout", GatewayTimeout(), 504,
			`{"error":"gateway_timeout","message":"upstream timed out"}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.resp.Status != c.status {
				t.Errorf("status = %d, want %d", c.resp.Status, c.status)
			}
			if got := string(c.resp.Body); got != c.body {
				t.Errorf("body = %s, want %s", got, c.body)
			}
			if !json.Valid(c.resp.Body) {
				t.Errorf("body is not valid JSON")
			}
			if ct := c.resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			if c.resp.FromUpstream {
				t.Errorf("FromUpstream = true for a gateway error")
			}
			for k, v := range c.header {
				if got := c.resp.Header.Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
		})
	}
}

func TestErrorResponseExtraOrderAndEscaping(t *testing.T) {
	r := ErrorResponse(418, "teapot", "a <b> & c", map[string]any{"z": 1, "a": "x"})
	want := `{"error":"teapot","message":"a <b> & c","a":"x","z":1}`
	if string(r.Body) != want {
		t.Fatalf("body = %s, want %s", r.Body, want)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestClassify(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	reset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	cases := []struct {
		name string
		resp *Response
		err  error
		want int
	}{
		{"upstream 200", &Response{Status: 200}, nil, 200},
		{"upstream 503", &Response{Status: 503}, nil, 503},
		{"upstream 404", &Response{Status: 404}, nil, 404},
		{"nil response", nil, nil, 502},
		{"deadline", nil, context.DeadlineExceeded, 504},
		{"wrapped deadline", nil, &url.Error{Op: "Get", URL: "http://x", Err: context.DeadlineExceeded}, 504},
		{"net timeout", nil, &url.Error{Op: "Get", URL: "http://x", Err: timeoutErr{}}, 504},
		{"client canceled", nil, &url.Error{Op: "Get", URL: "http://x", Err: context.Canceled}, StatusClientClosed},
		{"refused", nil, &url.Error{Op: "Get", URL: "http://x", Err: refused}, 502},
		{"reset", nil, reset, 502},
		{"eof", nil, fmt.Errorf("read body: %w", io.ErrUnexpectedEOF), 502},
		{"malformed", nil, errors.New(`malformed HTTP response "garbage"`), 502},
	}
	for _, c := range cases {
		if got := Classify(c.resp, c.err); got != c.want {
			t.Errorf("%s: Classify = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestParseVar(t *testing.T) {
	cases := map[string]Var{
		"plain":            Literal("plain"),
		"v=$request_time":  Literal("v=$request_time"),
		"$literal:gateway": Literal("gateway"),
		"$literal:$foo":    Literal("$foo"),
		"$request_time":    {Kind: VarRequestTime},
		"$response_time":   {Kind: VarResponseTime},
		"$route_path":      {Kind: VarRoutePath},
		"$body":            {Kind: VarBody},
	}
	for in, want := range cases {
		got, err := ParseVar(in)
		if err != nil || got != want {
			t.Errorf("ParseVar(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	if _, err := ParseVar("$requst_time"); err == nil {
		t.Errorf("ParseVar accepted an unknown token")
	}
}

func TestTargetProbe(t *testing.T) {
	tg, err := NewTarget("http://localhost:3003", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !tg.Healthy() {
		t.Fatal("new target not healthy")
	}
	tg.RecordProbe(false, 3)
	tg.RecordProbe(false, 3)
	if !tg.Healthy() {
		t.Fatal("unhealthy before threshold")
	}
	if tg.RecordProbe(false, 3) {
		t.Fatal("healthy after threshold failures")
	}
	if !tg.RecordProbe(true, 3) || tg.Failures() != 0 {
		t.Fatal("one success did not recover the target")
	}
	if _, err := NewTarget("localhost:3003", 1); err == nil {
		t.Fatal("NewTarget accepted a URL without scheme and host")
	}
}

func TestSystemClockSleepCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SystemClock{}).Sleep(ctx, 1<<40); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep = %v, want context.Canceled", err)
	}
}
