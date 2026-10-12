package server

// The mods' toasts ($.ui.toast, docs/SPEC.md §5.3): a short message a mod
// shows on the consoles. The server keeps each for a few seconds and
// sends it with session.list's Mods; a console shows the toasts it hasn't
// shown yet in its message line, once, its mod's name in front.

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Limits on toasts.
const (
	// toastTTL is how long the server keeps a toast for the consoles'
	// polls to see it.
	toastTTL = 10 * time.Second
	// toastMax is the most toasts kept, over all mods, and toastText a
	// toast's length in runes.
	toastMax  = 8
	toastText = 200
)

// modToasts are the toasts of the last toastTTL, oldest first.
type modToasts struct {
	mu   sync.Mutex
	seq  uint64
	kept []keptToast
}

type keptToast struct {
	at time.Time
	t  proto.ModToast
}

// add keeps a toast of mod id and answers its seq.
func (ts *modToasts) add(id, text, level string, now time.Time) uint64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.seq++
	ts.kept = append(ts.kept, keptToast{now, proto.ModToast{Seq: ts.seq, Mod: id, Text: text, Level: level}})
	if n := len(ts.kept) - toastMax; n > 0 {
		ts.kept = slices.Delete(ts.kept, 0, n)
	}
	return ts.seq
}

// list are the toasts not older than toastTTL of the mods show passes.
func (ts *modToasts) list(show func(id string) bool) []proto.ModToast {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	cut := time.Now().Add(-toastTTL)
	ts.kept = slices.DeleteFunc(ts.kept, func(k keptToast) bool { return k.at.Before(cut) })
	var out []proto.ModToast
	for _, k := range ts.kept {
		if show(k.t.Mod) {
			out = append(out, k.t)
		}
	}
	return out
}

// ui.toast {text, level}: a short message on the consoles, one line of
// the mod's text (control characters dropped, clipped); level info (the
// default), warn or error.
func (s *Server) modToast(_ context.Context, c *modbroker.Call) (any, error) {
	var a struct {
		Text  string `json:"text"`
		Level string `json:"level"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	text := modnotify.Line(a.Text, toastText)
	if text == "" {
		return nil, modbroker.BadParams("ui.toast: a toast needs a text")
	}
	if a.Level == "" {
		a.Level = "info"
	}
	if !slices.Contains(modnotify.Levels, a.Level) {
		return nil, modbroker.BadParams("ui.toast: level must be info, warn or error")
	}
	s.toasts.add(c.Mod.ID, text, a.Level, time.Now())
	return nil, nil
}
