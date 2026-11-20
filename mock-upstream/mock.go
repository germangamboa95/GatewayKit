package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	adminPrefix  = "/__mock"
	maxBodyBytes = 10 << 20
)

type echo struct {
	Service string            `json:"service"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
}

type logEntry struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	URI     string            `json:"uri"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
	Time    time.Time         `json:"time"`
}

type defaultOverride struct {
	Status  int `json:"status"`
	DelayMs int `json:"delay_ms"`
}

type mock struct {
	cfg config

	mu           sync.Mutex
	log          []logEntry
	failCounts   map[string]int
	healthStatus int
	override     *defaultOverride
}

func newMock(cfg config) *mock {
	m := &mock{cfg: cfg}
	m.reset()
	return m
}

func (m *mock) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log = []logEntry{}
	m.failCounts = map[string]int{}
	m.healthStatus = http.StatusOK
	m.override = nil
}

func (m *mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	received := time.Now().UTC()
	h := w.Header()
	h.Set("X-Mock-Service", m.cfg.Service)
	for _, kv := range m.cfg.ExtraHeaders {
		h.Set(kv[0], kv[1])
	}
	switch {
	case r.URL.Path == adminPrefix || strings.HasPrefix(r.URL.Path, adminPrefix+"/"):
		m.serveAdmin(w, r)
	case r.URL.Path == m.cfg.HealthPath:
		m.serveHealth(w)
	default:
		m.serveEcho(w, r, received)
	}
}

func (m *mock) serveHealth(w http.ResponseWriter) {
	m.mu.Lock()
	status := m.healthStatus
	m.mu.Unlock()
	writeJSON(w, status, map[string]any{"service": m.cfg.Service, "status": status})
}

func (m *mock) serveAdmin(w http.ResponseWriter, r *http.Request) {
	var method string
	switch r.URL.Path {
	case adminPrefix + "/requests":
		method = http.MethodGet
	case adminPrefix + "/reset", adminPrefix + "/health", adminPrefix + "/default":
		method = http.MethodPost
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown admin endpoint"})
		return
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	switch r.URL.Path {
	case adminPrefix + "/requests":
		m.mu.Lock()
		entries := append([]logEntry{}, m.log...)
		m.mu.Unlock()
		writeJSON(w, http.StatusOK, entries)
	case adminPrefix + "/reset":
		m.reset()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case adminPrefix + "/health":
		var req struct {
			Status int `json:"status"`
		}
		if err := decodeAdmin(r, &req); err != nil || !validStatus(req.Status) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be {\"status\": <200-599>}"})
			return
		}
		m.mu.Lock()
		m.healthStatus = req.Status
		m.mu.Unlock()
		writeJSON(w, http.StatusOK, req)
	case adminPrefix + "/default":
		var req defaultOverride
		if err := decodeAdmin(r, &req); err != nil || (req.Status != 0 && !validStatus(req.Status)) || req.DelayMs < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be {\"status\": <200-599>, \"delay_ms\": <n>=0>}"})
			return
		}
		if req.Status == 0 {
			req.Status = http.StatusOK
		}
		m.mu.Lock()
		m.override = &req
		m.mu.Unlock()
		writeJSON(w, http.StatusOK, req)
	}
}

func decodeAdmin(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (m *mock) serveEcho(w http.ResponseWriter, r *http.Request, received time.Time) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	body := bodyValue(raw)
	headers := flattenHeaders(r)
	query := flattenQuery(r)

	failTimes, hasFail := headerInt(r, "X-Mock-Fail-Times")
	key := r.Header.Get("X-Mock-Key")

	m.mu.Lock()
	m.log = append(m.log, logEntry{
		Method:  r.Method,
		Path:    r.URL.Path,
		URI:     r.RequestURI,
		Query:   query,
		Headers: headers,
		Body:    body,
		Time:    received,
	})
	var override defaultOverride
	if m.override != nil {
		override = *m.override
	}
	failing := false
	if hasFail {
		failing = m.failCounts[key] < failTimes
		m.failCounts[key]++
	}
	m.mu.Unlock()

	status := http.StatusOK
	if override.Status != 0 {
		status = override.Status
	}
	headerStatus, hasStatus := headerInt(r, "X-Mock-Status")
	hasStatus = hasStatus && validStatus(headerStatus)
	switch {
	case hasFail && failing:
		status = http.StatusServiceUnavailable
		if hasStatus {
			status = headerStatus
		}
	case hasFail:
	case hasStatus:
		status = headerStatus
	}

	delay := time.Duration(override.DelayMs) * time.Millisecond
	if d, ok := headerInt(r, "X-Mock-Delay-Ms"); ok && d >= 0 {
		delay = time.Duration(d) * time.Millisecond
	}
	if delay > 0 {
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			return
		}
	}

	if r.Header.Get("X-Mock-Drop") == "1" {
		drop(w)
		return
	}

	if text, ok := r.Header["X-Mock-Body"]; ok {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		io.WriteString(w, strings.Join(text, ", "))
		return
	}
	writeJSON(w, status, echo{
		Service: m.cfg.Service,
		Method:  r.Method,
		Path:    r.URL.Path,
		Query:   query,
		Headers: headers,
		Body:    body,
	})
}

func drop(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic(http.ErrAbortHandler)
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	conn.Close()
}

func bodyValue(raw []byte) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		if len(raw) == 0 {
			return nil
		}
		return string(raw)
	}
	if json.Valid(raw) {
		return json.RawMessage(raw)
	}
	return string(raw)
}

func flattenHeaders(r *http.Request) map[string]string {
	out := make(map[string]string, len(r.Header)+1)
	for name, values := range r.Header {
		out[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	if r.Host != "" {
		out["host"] = r.Host
	}
	return out
}

func flattenQuery(r *http.Request) map[string]string {
	q := r.URL.Query()
	out := make(map[string]string, len(q))
	for name, values := range q {
		out[name] = strings.Join(values, ",")
	}
	return out
}

func headerInt(r *http.Request, name string) (int, bool) {
	v := strings.TrimSpace(r.Header.Get(name))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

func validStatus(code int) bool {
	return code >= 200 && code <= 599
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}
