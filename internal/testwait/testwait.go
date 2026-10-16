// Package testwait holds the polling helpers tests share: wait until a
// condition holds, wait for the next event.
package testwait

import (
	"context"
	"testing"
	"time"
)

// Timeout is how long Eventually waits: generous, so a loaded machine or
// -race does not flake a test, and free when the condition holds early.
const Timeout = 10 * time.Second

const interval = 10 * time.Millisecond

// Poll calls cond until it returns true or timeout passes, and reports
// whether it did.
func Poll(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}

// Until fails the test unless cond holds within timeout.
func Until(t testing.TB, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	if !Poll(timeout, cond) {
		t.Fatalf("timed out waiting for %s", what)
	}
}

// Eventually is Until with Timeout.
func Eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	Until(t, what, Timeout, cond)
}

// Next returns the next event of src within d, or fails the test. src is
// anything with a context-bound Next, such as a modbus.Sub.
func Next[E any](t testing.TB, src interface {
	Next(context.Context) (E, error)
}, d time.Duration) E {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	ev, err := src.Next(ctx)
	if err != nil {
		t.Fatalf("no event within %v: %v", d, err)
	}
	return ev
}
