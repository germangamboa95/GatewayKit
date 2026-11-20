package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"gatewaykit/internal/contract"
)

var hopHeaders = []string{"Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

// RemoveHopHeaders deletes hop-by-hop headers, Proxy-* headers and any
// header named in Connection.
func RemoveHopHeaders(h http.Header) {
	for _, v := range h.Values("Connection") {
		for _, name := range strings.Split(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				h.Del(name)
			}
		}
	}
	for _, name := range hopHeaders {
		h.Del(name)
	}
	for name := range h {
		if strings.HasPrefix(textproto.CanonicalMIMEHeaderKey(name), "Proxy-") {
			delete(h, name)
		}
	}
}

// BuildRequest turns the request context into the outgoing request: method,
// rewritten path, buffered body, end-to-end headers and X-Forwarded-*.
// The target base path and Host are applied by the client.
func BuildRequest(ctx *contract.RequestContext) *contract.OutRequest {
	r := ctx.Req
	h := r.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	RemoveHopHeaders(h)
	h.Del("Host")

	ip := ctx.ClientIP
	if ip == "" {
		ip = r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		}
	}
	if ip != "" {
		if prior := h.Values("X-Forwarded-For"); len(prior) > 0 {
			ip = strings.Join(prior, ", ") + ", " + ip
		}
		h.Set("X-Forwarded-For", ip)
	}
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	h.Set("X-Forwarded-Proto", proto)
	h.Set("X-Forwarded-Host", r.Host)

	route := ctx.Route
	if route != nil && route.RespTransform != nil && route.RespTransform.Envelope != nil {
		h.Set("Accept-Encoding", "identity")
	}

	path := r.URL.Path
	strip := false
	routePath := "/"
	if route != nil {
		strip = route.StripPrefix
		routePath = route.Path
	}
	return &contract.OutRequest{
		Method: r.Method,
		Path:   RewritePath(routePath, strip, path, r.URL.RawQuery),
		Header: h,
		Body:   requestBody(ctx),
	}
}

func requestBody(ctx *contract.RequestContext) []byte {
	if ctx.Body != nil || ctx.Req.Body == nil || ctx.Req.Body == http.NoBody {
		return ctx.Body
	}
	b, err := io.ReadAll(ctx.Req.Body)
	if err == nil {
		ctx.Body = b
	}
	return b
}

// Client is the upstream HTTP client (design D7). It satisfies
// contract.UpstreamClient.
type Client struct {
	HTTP *http.Client
}

var sharedTransport = &http.Transport{
	DialContext:         (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext,
	DisableCompression:  true,
	MaxIdleConns:        256,
	MaxIdleConnsPerHost: 64,
	IdleConnTimeout:     90 * time.Second,
}

// NewClient returns a Client on the shared transport that never follows redirects.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Transport: sharedTransport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Send performs one request against t and buffers the whole response body
// inside ctx. Errors after ctx ends wrap ctx.Err().
func (c *Client) Send(ctx context.Context, t *contract.Target, r *contract.OutRequest) (*contract.Response, error) {
	u := *t.URL
	full := JoinPath(t.URL.Path, r.Path)
	path, query, _ := strings.Cut(full, "?")
	u.Path = path
	u.RawPath = ""
	u.RawQuery = query
	u.Fragment = ""

	var body io.Reader = http.NoBody
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, t.URL.Scheme+"://"+t.URL.Host, body)
	if err != nil {
		return nil, err
	}
	req.URL = &u
	if r.Header != nil {
		req.Header = r.Header.Clone()
	}
	req.Header.Del("Host")
	req.Host = t.URL.Host
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header["User-Agent"] = nil
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, wrapCtx(ctx, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, wrapCtx(ctx, err)
	}
	h := resp.Header.Clone()
	RemoveHopHeaders(h)
	return &contract.Response{Status: resp.StatusCode, Header: h, Body: b, FromUpstream: true}, nil
}

func wrapCtx(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("%w: %v", cerr, err)
	}
	return err
}
