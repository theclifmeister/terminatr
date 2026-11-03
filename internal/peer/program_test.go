package peer

import (
	"slices"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The hello lists CapProgram only while programs are on, and only on a
// server that takes messages.
func TestProgramCap(t *testing.T) {
	fm := &fakeMessages{}
	for _, c := range []struct {
		opts Options
		want bool
	}{
		{Options{Messages: fm}, false},
		{Options{Messages: fm, Programs: true}, true},
		{Options{Programs: true}, false},
	} {
		if got := slices.Contains((&Manager{opts: c.opts}).caps(), CapProgram); got != c.want {
			t.Errorf("programs %v, messages %v: cap %v", c.opts.Programs, c.opts.Messages != nil, got)
		}
	}
}

// Switching programs reaches a live link at once: the link dials again
// and the paired server reads the new hello, with CapProgram and then
// without; the same switch again leaves the link alone.
func TestProgramCapOverLink(t *testing.T) {
	_, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	b.pair(t, "mbp", a, url)
	start := func(s *server, on bool) *Manager {
		m := Start(Options{Dir: s.dir, Version: "1", Logf: t.Logf, RelayTLS: tc, Messages: &fakeMessages{}, Programs: on,
			Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
		t.Cleanup(m.Close)
		return m
	}
	ma, mb := start(a, false), start(b, true)
	has := func(p proto.PeerLink) bool { return slices.Contains(p.Caps, CapProgram) }
	waitLink(t, mb, "mbp", "up without program", func(p proto.PeerLink) bool { return isUp(p) && !has(p) })
	waitLink(t, ma, "mini", "up with program", func(p proto.PeerLink) bool { return isUp(p) && has(p) })

	ma.SetPrograms(true)
	if !ma.Programs() {
		t.Fatal("Programs after SetPrograms(true)")
	}
	waitLink(t, mb, "mbp", "up with program", func(p proto.PeerLink) bool { return isUp(p) && has(p) })
	since := waitLink(t, mb, "mbp", "up", isUp).Since
	ma.SetPrograms(true)
	time.Sleep(200 * time.Millisecond)
	if l := waitLink(t, mb, "mbp", "still up", isUp); !l.Since.Equal(since) {
		t.Errorf("the same switch again dialled again: up since %v, was %v", l.Since, since)
	}

	ma.SetPrograms(false)
	waitLink(t, mb, "mbp", "up without program", func(p proto.PeerLink) bool { return isUp(p) && !has(p) })
}
