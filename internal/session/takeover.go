package session

import (
	"sync/atomic"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// boxWatchWait is how long text a person typed has to show in the
// agent's prompt box to count as theirs (HumanInput): a key that went
// elsewhere (a dialog's answer, a menu) leaves the box as it was. Atomic
// as session goroutines read it while tests set it.
var boxWatchWaitNS atomic.Int64

func init() { boxWatchWaitNS.Store(int64(3 * time.Second)) }

func boxWatchWait() time.Duration { return time.Duration(boxWatchWaitNS.Load()) }

// boxWatch waits for a person's text to show in the prompt box.
type boxWatch struct {
	since time.Time
	took  func()
	// box: no dialog was up when the text was typed; sent: Enter
	// followed it, which may send it before any screen reading shows
	// it in the box, so the agent starting its turn shows it too.
	box, sent bool
}

// HumanInput is Input from a person's console. When p holds text, a
// text key or a paste (emu.Text), took runs once that text shows in the
// agent's prompt box as the person's (docs/SPEC.md §4, takeover): the
// screen rules read a box that isn't empty, the agent's own dim
// suggestion or placeholder counting as empty (the manifest's
// empty_rule skips dim text), with no dialog up; or, the text typed
// with no dialog up and sent with Enter before a reading showed it, the
// agent starting a turn. An agent whose box tm can't read (no
// empty_rule), or no agent, runs it at once. Clicks, the wheel, focus,
// arrows, page keys, Esc, Enter and control keys never run it, nor do
// tm's own keys, which come through Input. took runs on the agent's
// goroutine or the caller's, and mustn't block; nil skips the watch.
func (s *Session) HumanInput(p []byte, took func()) error {
	if err := s.Input(p); err != nil {
		return err
	}
	text, enter := took != nil && emu.Text(p), submits(p)
	if !text && !enter {
		return nil
	}
	rt := s.agentRT()
	if rt == nil || rt.man == nil || rt.man.Inject.EmptyRule == "" {
		if text {
			took()
		}
		return nil
	}
	rt.mu.Lock()
	if text {
		rt.boxWatches = append(rt.boxWatches, boxWatch{since: time.Now(), took: took, box: !rt.blocker})
	}
	if enter {
		for i := range rt.boxWatches {
			rt.boxWatches[i].sent = rt.boxWatches[i].box
		}
	}
	rt.mu.Unlock()
	return nil
}

// submits says whether p holds Enter, plain or as a kitty key report.
func submits(p []byte) bool {
	for len(p) > 0 {
		kind, n := emu.NextInput(p)
		if k := string(p[:n]); kind == emu.InputKey && (k == "\r" || k == "\x1b[13u" || k == "\x1b[13;1u") {
			return true
		}
		p = p[n:]
	}
	return false
}

// settleBoxWatches takes the watches a screen read at settles: those
// from before it when the box held text then (held), and those sent
// when the agent is working, which it returns to run, and those older
// than boxWatchWait, whose text never showed. rt.mu held.
func (rt *agentRT) settleBoxWatches(at time.Time, held, working bool) []func() {
	var run []func()
	kept := rt.boxWatches[:0]
	for _, w := range rt.boxWatches {
		switch {
		case held && !w.since.After(at), working && w.sent:
			run = append(run, w.took)
		case at.Sub(w.since) <= boxWatchWait():
			kept = append(kept, w)
		}
	}
	clear(rt.boxWatches[len(kept):])
	rt.boxWatches = kept
	return run
}
