package session

import (
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// boxWatchWait is how long text a person typed has to show in the
// agent's prompt box to count as theirs (HumanInput): a key that went
// elsewhere (a dialog's answer, a menu) leaves the box as it was.
var boxWatchWait = 3 * time.Second

// boxWatch waits for a person's text to show in the prompt box.
type boxWatch struct {
	since time.Time
	took  func()
}

// HumanInput is Input from a person's console. When p holds text, a
// text key or a paste (emu.Text), took runs once that text shows in the
// agent's prompt box as the person's (docs/SPEC.md §4, takeover): the
// screen rules read a box that isn't empty, the agent's own dim
// suggestion or placeholder counting as empty (the manifest's
// empty_rule skips dim text), with no dialog up. An agent whose box tm
// can't read (no empty_rule), or no agent, runs it at once. Clicks, the
// wheel, focus, arrows, page keys, Esc, Enter and control keys never
// run it, nor do tm's own keys, which come through Input. took runs on
// the agent's goroutine or the caller's, and mustn't block; nil skips
// the watch.
func (s *Session) HumanInput(p []byte, took func()) error {
	if err := s.Input(p); err != nil || took == nil || !emu.Text(p) {
		return err
	}
	rt := s.agentRT()
	if rt == nil || rt.man == nil || rt.man.Inject.EmptyRule == "" {
		took()
		return nil
	}
	rt.mu.Lock()
	rt.boxWatches = append(rt.boxWatches, boxWatch{since: time.Now(), took: took})
	rt.mu.Unlock()
	return nil
}

// settleBoxWatches takes the watches a screen read at settles: those
// from before it when the box held text then (held), which it returns
// to run, and those older than boxWatchWait, whose text never showed.
// rt.mu held.
func (rt *agentRT) settleBoxWatches(at time.Time, held bool) []func() {
	var run []func()
	kept := rt.boxWatches[:0]
	for _, w := range rt.boxWatches {
		switch {
		case held && !w.since.After(at):
			run = append(run, w.took)
		case at.Sub(w.since) <= boxWatchWait:
			kept = append(kept, w)
		}
	}
	clear(rt.boxWatches[len(kept):])
	rt.boxWatches = kept
	return run
}
