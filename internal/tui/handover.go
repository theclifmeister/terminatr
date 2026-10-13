package tui

import (
	"bytes"
	"os"
	"sync"
	"time"
)

// The dashboard and the attach view are two programs taking turns on
// one outer terminal (cli's console loop). Each used to set the screen up
// and tear it down: leave the alternate screen (the shell's screen showed
// through), enter it again blank, then draw, unsynchronised on the
// dashboard's side. Every switch between them flashed (T372). Now the
// alternate screen stays on across a switch: the one leaving keeps its
// last frame up, cursor hidden, and the next draws its first frame over
// it in one synchronised update.

// holdMax is how long a dashboard handed the screen keeps the attach's
// last frame up while its data loads: a slow server still gets its
// "loading…" drawn.
const holdMax = 300 * time.Millisecond

// holdFrame is how long after its data a held dashboard waits for the
// frame that draws it.
const holdFrame = 50 * time.Millisecond

const (
	syncStart = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
	leaveAlt  = "\x1b[?1049l"
	showCur   = "\x1b[?25h"
)

// dashOut is the dashboard's output. It writes every write as one
// synchronised update (mode 2026): Bubble Tea synchronises its frames
// only once the terminal answered its query, after the first frame, and
// over SSH only in the terminals it knows. It never leaves the alternate screen (Bubble Tea's close
// does): Dashboard does once it quits, and on attaching the screen stays
// up for the attach to draw over. Handed the screen (DashOptions.Handover)
// it doesn't enter the alternate screen, already on, and holds its output
// until the dashboard has its data (loaded) or holdMax passed, so the
// attach's frame stays up rather than an empty dashboard.
type dashOut struct {
	*os.File // a terminal: Bubble Tea sizes it and makes it raw

	mu      sync.Mutex
	enter   bool   // the alternate screen is on already: drop the next enterScreen
	holding bool   // output waits in held
	held    []byte // output held back, in order
}

func newDashOut(f *os.File, handover bool) *dashOut {
	o := &dashOut{File: f, enter: handover, holding: handover}
	if handover {
		time.AfterFunc(holdMax, o.flush)
	}
	return o
}

func (o *dashOut) Write(b []byte) (int, error) {
	n := len(b)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.enter && bytes.Contains(b, []byte(enterScreen)) {
		b = bytes.Replace(b, []byte(enterScreen), nil, 1)
		o.enter = false
	}
	if bytes.Contains(b, []byte(leaveAlt)) {
		// Bubble Tea's close: the screen stays, the cursor hidden.
		b = bytes.ReplaceAll(b, []byte(leaveAlt), nil)
		b = bytes.ReplaceAll(b, []byte(showCur), nil)
	}
	if o.holding {
		o.held = append(o.held, b...)
		return n, nil
	}
	if len(o.held) > 0 {
		b, o.held = append(o.held, b...), nil
	}
	if _, err := o.File.Write(synced(b)); err != nil {
		return 0, err
	}
	return n, nil
}

// loaded ends the hold once the dashboard has its data: the frame that
// draws it carries what was held, one update. Should none come, what was
// held is written shortly after.
func (o *dashOut) loaded() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.holding {
		o.holding = false
		time.AfterFunc(holdFrame, o.flush)
	}
}

// flush writes what is still held.
func (o *dashOut) flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holding = false
	if len(o.held) > 0 {
		o.File.Write(synced(o.held))
		o.held = nil
	}
}

// synced is b as one synchronised update: Bubble Tea's own updates in it
// (held writes, or one it synchronised itself) become part of it.
func synced(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	b = bytes.ReplaceAll(b, []byte(syncStart), nil)
	b = bytes.ReplaceAll(b, []byte(syncEnd), nil)
	out := make([]byte, 0, len(b)+len(syncStart)+len(syncEnd))
	out = append(out, syncStart...)
	out = append(out, b...)
	return append(out, syncEnd...)
}
