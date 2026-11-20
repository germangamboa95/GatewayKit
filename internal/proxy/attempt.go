package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gatewaykit/internal/contract"
)

// Attempt sends r to t with a per-attempt timeout derived from parent. The
// client must read the full body inside the context it gets. A timeout error
// wraps context.DeadlineExceeded; a parent cancel wraps context.Canceled.
func Attempt(parent context.Context, c contract.UpstreamClient, t *contract.Target, r *contract.OutRequest, timeout time.Duration) (*contract.Response, error) {
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

// Outcome maps an attempt to its status (contract.Classify) and the response
// to return: the upstream response as-is, or a gateway 502/504. For a client
// cancel the status is contract.StatusClientClosed and the response is an
// empty placeholder no client will read.
func Outcome(resp *contract.Response, err error) (*contract.Response, int) {
	status := contract.Classify(resp, err)
	switch {
	case err == nil && resp != nil:
		return resp, status
	case status == http.StatusGatewayTimeout:
		return contract.GatewayTimeout(), status
	case status == contract.StatusClientClosed:
		return ClientClosed(), status
	default:
		return contract.BadGateway(), http.StatusBadGateway
	}
}

// ClientClosed is the placeholder returned when the client went away.
func ClientClosed() *contract.Response {
	return &contract.Response{Status: contract.StatusClientClosed, Header: http.Header{}}
}
