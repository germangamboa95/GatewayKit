package balance

import "github.com/germangamboa95/gatewaykit/internal/config"

var _ TargetPool = (*Pool)(nil)

// Pool is a route's target list with shared health state.
type Pool struct {
	targets   []*config.Target
	checked   bool
	threshold int
}

// NewPool returns the pool for route. Without a health check every target
// is always healthy.
func NewPool(route *config.Route) *Pool {
	p := &Pool{targets: route.Targets}
	if hc := route.HealthCheck; hc != nil {
		p.checked = true
		p.threshold = hc.UnhealthyThreshold
		if p.threshold < 1 {
			p.threshold = 3
		}
	}
	return p
}

// Targets returns the route's targets in config order.
func (p *Pool) Targets() []*config.Target { return p.targets }

// Healthy reports whether t is in rotation. Without a health check every
// target is.
func (p *Pool) Healthy(t *config.Target) bool {
	return !p.checked || t.Healthy()
}

// MarkProbe applies one probe result using the route's unhealthy threshold.
func (p *Pool) MarkProbe(t *config.Target, ok bool) {
	if p.checked {
		t.RecordProbe(ok, p.threshold)
	}
}
