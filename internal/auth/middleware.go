package auth

import (
	"errors"
	"net/http"

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

// Middleware returns the auth stage for cfg. A nil cfg means no auth: the
// stage passes every request through.
func Middleware(cfg *config.Auth) gateway.Middleware {
	if cfg == nil {
		return gateway.PassThrough
	}
	a := FromConfig(cfg)
	return func(next gateway.Handler) gateway.Handler {
		return func(rc *gateway.RequestContext) *gateway.Response {
			var err error
			if rc.Req == nil {
				err = ErrMissingAPIKey
			} else {
				err = a.Check(rc.Req.Header)
			}
			if err != nil {
				return Unauthorized(err)
			}
			return next(rc)
		}
	}
}

// Unauthorized maps ErrMissingAPIKey or ErrInvalidAPIKey to its 401 body.
func Unauthorized(err error) *gateway.Response {
	msg := ErrInvalidAPIKey.Error()
	if errors.Is(err, ErrMissingAPIKey) {
		msg = ErrMissingAPIKey.Error()
	}
	return gateway.ErrorResponse(http.StatusUnauthorized, gateway.CodeUnauthorized, msg, nil)
}
