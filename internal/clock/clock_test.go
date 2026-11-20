package clock

import (
	"context"
	"errors"
	"testing"
)

func TestSystemSleepCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (System{}).Sleep(ctx, 1<<40); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep = %v, want context.Canceled", err)
	}
}
