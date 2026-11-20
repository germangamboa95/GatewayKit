package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

// Request is what the upstream client sends. Path includes the query string.
type Request struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// Sender sends one request to a target and returns the buffered response.
// *Client implements it.
type Sender interface {
	Send(ctx context.Context, t *config.Target, r *Request) (*gateway.Response, error)
}

// Attempt sends r to t with a per-attempt timeout derived from parent. The
// client must read the full body inside the context it gets. A timeout error
// wraps context.DeadlineExceeded; a parent cancel wraps context.Canceled.
func Attempt(parent context.Context, c Sender, t *config.Target, r *Request, timeout time.Duration) (*gateway.Response, error) {
	ctx, cancel := parent, context.CancelFunc(func() {})
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	defer cancel()
	resp, err := c.Send(ctx, t, r)
	if err == nil {
		return resp, nil
	}
	if parent.Err() != nil {
		return nil, errors.Join(context.Canceled, err)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errors.Join(context.DeadlineExceeded, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// A context error the attempt did not cause: not a client cancel, not our timeout.
		return nil, fmt.Errorf("upstream attempt failed: %v", err)
	}
	return nil, err
}

// Outcome maps an attempt to its status (Classify) and the response
// to return: the upstream response as-is, or a gateway 502/504. For a client
// cancel the status is gateway.StatusClientClosed and the response is an
// empty placeholder no client will read.
func Outcome(resp *gateway.Response, err error) (*gateway.Response, int) {
	status := Classify(resp, err)
	switch {
	case err == nil && resp != nil:
		return resp, status
	case status == http.StatusGatewayTimeout:
		return gateway.GatewayTimeout(), status
	case status == gateway.StatusClientClosed:
		return ClientClosed(), status
	default:
		return gateway.BadGateway(), http.StatusBadGateway
	}
}

// ClientClosed is the placeholder returned when the client went away.
func ClientClosed() *gateway.Response {
	return &gateway.Response{Status: gateway.StatusClientClosed, Header: http.Header{}}
}

// Classify maps an upstream attempt to a status: the response status, 504 for
// a timeout, gateway.StatusClientClosed for a client cancel, 502 for any
// other error.
func Classify(resp *gateway.Response, err error) int {
	if err == nil {
		if resp == nil {
			return http.StatusBadGateway
		}
		return resp.Status
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return http.StatusGatewayTimeout
	}
	if errors.Is(err, context.Canceled) {
		return gateway.StatusClientClosed
	}
	return http.StatusBadGateway
}
