package balance

import "gatewaykit/internal/contract"

var _ contract.TargetPool = (*Pool)(nil)

// Pool is a route's target list with shared health state.
type Pool struct {
	targets   []*contract.Target
	checked   bool
	threshold int
}

// NewPool returns the pool for route. Without a health check every target
// is always healthy.
func NewPool(route *contract.Route) *Pool {
	p := &Pool{targets: route.Targets}
	if hc := route.HealthCheck; hc != nil {
		p.checked = true
		p.threshold = hc.UnhealthyThreshold
		if p.threshold < 1 {
			p.threshold = 1
		}
	}
	return p
}

func (p *Pool) Targets() []*contract.Target { return p.targets }

func (p *Pool) Healthy(t *contract.Target) bool {
	return !p.checked || t.Healthy()
}

// MarkProbe applies one probe result using the route's unhealthy threshold.
func (p *Pool) MarkProbe(t *contract.Target, ok bool) {
	if p.checked {
		t.RecordProbe(ok, p.threshold)
	}
}
