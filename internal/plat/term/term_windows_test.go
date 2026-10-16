//go:build windows

package term

import (
	"context"
	"testing"
)

func TestEventsEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	evs := Events(ctx)
	cancel()
	for range evs {
	}
}
