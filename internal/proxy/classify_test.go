package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"

	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestClassify(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	reset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	cases := []struct {
		name string
		resp *gateway.Response
		err  error
		want int
	}{
		{"upstream 200", &gateway.Response{Status: 200}, nil, 200},
		{"upstream 503", &gateway.Response{Status: 503}, nil, 503},
		{"upstream 404", &gateway.Response{Status: 404}, nil, 404},
		{"nil response", nil, nil, 502},
		{"deadline", nil, context.DeadlineExceeded, 504},
		{"wrapped deadline", nil, &url.Error{Op: "Get", URL: "http://x", Err: context.DeadlineExceeded}, 504},
		{"net timeout", nil, &url.Error{Op: "Get", URL: "http://x", Err: timeoutErr{}}, 504},
		{"client canceled", nil, &url.Error{Op: "Get", URL: "http://x", Err: context.Canceled}, gateway.StatusClientClosed},
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
