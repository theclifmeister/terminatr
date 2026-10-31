package peer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAnswerTooBig (T892): an answer over the line a link carries goes
// as a too-big error naming the method and size, logged on the side
// that didn't send it, and the link stays up for the next call.
func TestAnswerTooBig(t *testing.T) {
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ca.Close(); cb.Close() })
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	var mu sync.Mutex
	var logs []string
	sb.logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	sb.methods = map[string]handler{
		"peer.big":   func(context.Context, json.RawMessage) (any, error) { return strings.Repeat("x", maxLine), nil },
		"peer.small": func(context.Context, json.RawMessage) (any, error) { return "ok", nil },
	}
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	ctx := context.Background()
	err = sa.call(ctx, "peer.big", nil, nil)
	if code(err) != proto.ErrTooBig || !strings.Contains(err.Error(), "peer.big: its answer is 1048578 bytes, over the 1048576") {
		t.Fatalf("big: %v", err)
	}
	var got string
	if err := sa.call(ctx, "peer.small", nil, &got); err != nil || got != "ok" {
		t.Fatalf("after it: %q %v", got, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 || logs[0] != "peer.big's answer is 1048578 bytes, over the link's 1048576: not sent" {
		t.Fatalf("logs %q", logs)
	}
}
