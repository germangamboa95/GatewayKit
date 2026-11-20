// Package clock abstracts the current time and sleeping, so tests can
// control both.
package clock

import (
	"context"
	"time"
)

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// Sleeper waits for d or until ctx is done, returning ctx.Err() in that case.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// System is the real Clock and Sleeper.
type System struct{}

// Now returns time.Now().
func (System) Now() time.Time { return time.Now() }

// Sleep waits for d or until ctx is done.
func (System) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
