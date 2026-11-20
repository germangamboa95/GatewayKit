package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// MockPorts are the Compose mock services: users, orders, products-a, products-b, legacy, internal.
var MockPorts = []int{3001, 3002, 3003, 3004, 3005, 3006}

// GatewayURL returns GATEWAY_URL without a trailing slash, or "" when unset.
func GatewayURL() string {
	u := os.Getenv("GATEWAY_URL")
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}

// Config returns CONFIG, defaulting to gateway.yaml.
func Config() string {
	if c := os.Getenv("CONFIG"); c != "" {
		return c
	}
	return "gateway.yaml"
}

// MockHost returns MOCK_HOST, defaulting to localhost.
func MockHost() string {
	if h := os.Getenv("MOCK_HOST"); h != "" {
		return h
	}
	return "localhost"
}

// MockURL returns the base URL of the mock on port.
func MockURL(port int) string {
	return "http://" + net.JoinHostPort(MockHost(), strconv.Itoa(port))
}

// RequireConfig skips the test unless GATEWAY_URL is set and CONFIG is one of configs.
func RequireConfig(t testing.TB, configs ...string) {
	t.Helper()
	if GatewayURL() == "" {
		t.Skip("GATEWAY_URL not set; e2e needs the Compose stack")
	}
	if !slices.Contains(configs, Config()) {
		t.Skipf("CONFIG=%s; scenario applies to %v", Config(), configs)
	}
}

var (
	nextIP      atomic.Uint32
	srcIPOnce   sync.Once
	srcIPUsable bool
)

func init() { nextIP.Store(1) }

// SourceIPsUsable reports whether this host can bind 127.0.0.N for N > 1 (Linux yes, macOS no).
func SourceIPsUsable() bool {
	srcIPOnce.Do(func() {
		if os.Getenv("E2E_SOURCE_IP") == "off" {
			return
		}
		l, err := net.Listen("tcp", "127.0.0.2:0")
		if err == nil {
			l.Close()
			srcIPUsable = true
		}
	})
	return srcIPUsable
}

// RequireDistinctIPs skips the test when per-test source IPs are unavailable.
func RequireDistinctIPs(t testing.TB) {
	t.Helper()
	if !SourceIPsUsable() {
		t.Skip("cannot bind 127.0.0.N source addresses on this host")
	}
}

func allocIP() string {
	i := nextIP.Add(1) - 2
	if i >= 253*255 {
		panic("e2e: source IP pool exhausted")
	}
	return fmt.Sprintf("127.0.%d.%d", i/253, i%253+2)
}

// Client sends requests from its own 127.0.0.N source address.
type Client struct {
	T    testing.TB
	IP   string
	Base string
	HC   *http.Client
}

// NewClient returns a gateway client bound to a fresh source IP.
func NewClient(t testing.TB) *Client {
	t.Helper()
	return NewClientFor(t, GatewayURL())
}

// NewClientFor returns a client for base bound to a fresh source IP.
func NewClientFor(t testing.TB, base string) *Client {
	t.Helper()
	d := &net.Dialer{Timeout: 5 * time.Second}
	ip := "127.0.0.1"
	if SourceIPsUsable() {
		ip = allocIP()
		d.LocalAddr = &net.TCPAddr{IP: net.ParseIP(ip)}
	}
	tr := &http.Transport{
		DialContext:        d.DialContext,
		DisableCompression: true,
		MaxIdleConns:       16,
		IdleConnTimeout:    30 * time.Second,
	}
	t.Cleanup(tr.CloseIdleConnections)
	return &Client{
		T:    t,
		IP:   ip,
		Base: base,
		HC: &http.Client{
			Transport:     tr,
			Timeout:       60 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Resp is a fully read response.
type Resp struct {
	Status  int
	Header  http.Header
	Body    []byte
	Start   time.Time
	Elapsed time.Duration
}

// JSON decodes the body into v, failing the test on error.
func (r *Resp) JSON(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode body %q: %v", r.Body, err)
	}
}

// Map decodes the body as a JSON object.
func (r *Resp) Map(t testing.TB) map[string]any {
	t.Helper()
	var m map[string]any
	r.JSON(t, &m)
	return m
}

// Req builds and sends a request; body may be nil, string, []byte or any JSON-encodable value.
// hdr pairs are name, value, name, value; a "Host" name sets req.Host.
func (c *Client) Req(method, path string, body any, hdr ...string) *Resp {
	c.T.Helper()
	r, err := c.Try(method, path, body, hdr...)
	if err != nil {
		c.T.Fatalf("%s %s: %v", method, path, err)
	}
	return r
}

// Try is Req that returns transport errors instead of failing.
func (c *Client) Try(method, path string, body any, hdr ...string) (*Resp, error) {
	var rd io.Reader
	isJSON := false
	switch b := body.(type) {
	case nil:
	case string:
		rd = bytes.NewBufferString(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		enc, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(enc)
		isJSON = true
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.Base+path, rd)
	if err != nil {
		return nil, err
	}
	if isJSON {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(hdr)%2 != 0 {
		return nil, fmt.Errorf("odd header list %v", hdr)
	}
	for i := 0; i < len(hdr); i += 2 {
		if http.CanonicalHeaderKey(hdr[i]) == "Host" {
			req.Host = hdr[i+1]
			continue
		}
		req.Header.Add(hdr[i], hdr[i+1])
	}
	return c.Do(req)
}

// Do sends req and reads the whole body.
func (c *Client) Do(req *http.Request) (*Resp, error) {
	start := time.Now()
	res, err := c.HC.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return &Resp{Status: res.StatusCode, Header: res.Header, Body: b, Start: start, Elapsed: time.Since(start)}, nil
}

// Get is Req("GET", path, nil, hdr...).
func (c *Client) Get(path string, hdr ...string) *Resp {
	c.T.Helper()
	return c.Req(http.MethodGet, path, nil, hdr...)
}

// MockRequest is one entry of GET /__mock/requests.
type MockRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	URI     string            `json:"uri"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
	Time    time.Time         `json:"time"`
}

// Echo is the mock's default response body.
type Echo struct {
	Service string            `json:"service"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
}

var adminClient = &http.Client{Timeout: 10 * time.Second}

func mockAdmin(t testing.TB, port int, method, path string, body any) []byte {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, MockURL(port)+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adminClient.Do(req)
	if err != nil {
		t.Fatalf("mock %d %s %s: %v", port, method, path, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("mock %d %s %s: status %d: %s", port, method, path, res.StatusCode, out)
	}
	return out
}

// ResetMocks resets the given mocks, or all six when none are given.
func ResetMocks(t testing.TB, ports ...int) {
	t.Helper()
	if len(ports) == 0 {
		ports = MockPorts
	}
	for _, p := range ports {
		mockAdmin(t, p, http.MethodPost, "/__mock/reset", nil)
	}
}

// SetMockHealth sets the status the mock's health path returns.
func SetMockHealth(t testing.TB, port, status int) {
	t.Helper()
	mockAdmin(t, port, http.MethodPost, "/__mock/health", map[string]int{"status": status})
}

// SetMockDefault makes every non-admin, non-health request return status after delayMs.
func SetMockDefault(t testing.TB, port, status, delayMs int) {
	t.Helper()
	mockAdmin(t, port, http.MethodPost, "/__mock/default", map[string]int{"status": status, "delay_ms": delayMs})
}

// MockRequests returns the mock's request log in arrival order.
func MockRequests(t testing.TB, port int) []MockRequest {
	t.Helper()
	var out []MockRequest
	if err := json.Unmarshal(mockAdmin(t, port, http.MethodGet, "/__mock/requests", nil), &out); err != nil {
		t.Fatalf("mock %d requests: %v", port, err)
	}
	return out
}

// LastMockRequest returns the newest logged request, failing if the log is empty.
func LastMockRequest(t testing.TB, port int) MockRequest {
	t.Helper()
	reqs := MockRequests(t, port)
	if len(reqs) == 0 {
		t.Fatalf("mock %d: no requests logged", port)
	}
	return reqs[len(reqs)-1]
}

// Gaps returns the time between consecutive logged requests.
func Gaps(reqs []MockRequest) []time.Duration {
	var g []time.Duration
	for i := 1; i < len(reqs); i++ {
		g = append(g, reqs[i].Time.Sub(reqs[i-1].Time))
	}
	return g
}

// Within fails unless got is within tol of want.
func Within(t testing.TB, what string, got, want, tol time.Duration) {
	t.Helper()
	if got < want-tol || got > want+tol {
		t.Errorf("%s = %v, want %v ± %v", what, got, want, tol)
	}
}

// Timed runs f and returns how long it took.
func Timed(f func()) time.Duration {
	start := time.Now()
	f()
	return time.Since(start)
}

// Eventually polls cond every interval until it returns true or timeout passes.
func Eventually(t testing.TB, timeout, interval time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(interval)
	}
}

// WindowBoundary returns the next clock-aligned boundary, floor(now/window)+1 windows.
func WindowBoundary(now time.Time, window time.Duration) time.Time {
	return now.Truncate(window).Add(window)
}

// WaitWindowStart sleeps until just after the next fixed-window boundary (50ms margin).
func WaitWindowStart(t testing.TB, window time.Duration) {
	t.Helper()
	time.Sleep(time.Until(WindowBoundary(time.Now(), window)) + 50*time.Millisecond)
}

// WaitWindowRoom waits for the next window when less than need remains in the current one.
func WaitWindowRoom(t testing.TB, window, need time.Duration) {
	t.Helper()
	if time.Until(WindowBoundary(time.Now(), window)) < need {
		WaitWindowStart(t, window)
	}
}

// RestoreMockHealth sets the mock's health back to 200 and, if probe is non-nil,
// waits up to timeout for probe to report the target healthy at the gateway.
func RestoreMockHealth(t testing.TB, port int, timeout time.Duration, probe func() bool) {
	t.Helper()
	SetMockHealth(t, port, http.StatusOK)
	if probe != nil {
		Eventually(t, timeout, 100*time.Millisecond, fmt.Sprintf("mock %d healthy at gateway", port), probe)
	}
}

// WaitBreakerCooldown resets the mock, sleeps out cooldown plus a margin, then sends
// probe (if non-nil) so the half-open breaker closes on a success.
func WaitBreakerCooldown(t testing.TB, port int, cooldown time.Duration, probe func() *Resp) {
	t.Helper()
	ResetMocks(t, port)
	time.Sleep(cooldown + 200*time.Millisecond)
	if probe != nil {
		Eventually(t, 5*time.Second, 200*time.Millisecond, "breaker closed", func() bool {
			return probe().Status < 500
		})
	}
}

// ErrorBody is the gateway's JSON error contract.
type ErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// RequireError asserts status, application/json and the error code with a message string.
func RequireError(t testing.TB, r *Resp, status int, code string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status = %d, want %d; body %s", r.Status, status, r.Body)
	}
	if ct := r.Header.Get("Content-Type"); !isJSONType(ct) {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var m map[string]any
	r.JSON(t, &m)
	if m["error"] != code {
		t.Errorf("error = %v, want %q; body %s", m["error"], code, r.Body)
	}
	if _, ok := m["message"].(string); !ok {
		t.Errorf("message missing or not a string; body %s", r.Body)
	}
}

func isJSONType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && mt == "application/json"
}
