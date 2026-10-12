// Package modnotify holds the OS notifications user mods send with
// $.ui.notify (mods design §5 and §7, the notify grant). The server keeps
// them in a Hub; every console shows them, one console draws the OS
// notification, and a button on one is a core action id on a task, bound as
// a mod's row binds one (modaction): pressing it goes through the console's
// action binding, with its reach check, live re-check and the core's own
// confirmation. The mod never acts, and what it writes is data: its text is
// clipped to one line and stripped of control characters (an escape
// sequence would reach the terminal), and its title is marked with the
// mod's id by the console, never by the mod.
package modnotify

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/modaction"
)

// Limits on what a mod sends.
const (
	MaxTitle   = 60
	MaxText    = 240
	MaxButtons = 3
	// MaxQuestion bounds a question.answer button's question text.
	MaxQuestion = 1024
	// PerMinute is how many notifications one mod may send a minute, and
	// MaxKept how many of one mod's stay in the tray (older ones go).
	PerMinute = 6
	MaxKept   = 5
	// MaxAll bounds the tray over all mods.
	MaxAll = 20
	// TTL is how long a notification stays in the tray.
	TTL = 30 * time.Minute
)

// Levels are the levels a notification has; "" is info.
var Levels = []string{"info", "warn", "error"}

// Action is the core action a button may name, id: one of the action
// binding's (modaction, the mods design §11) that takes a project, so it
// can carry the button's task, or question.answer (mod API 1.3), which
// carries a session's question and one of its options. Its label, key
// and confirmation are the core's; a mod can't word a button.
func Action(id string) (modaction.Action, bool) {
	a, ok := modaction.Lookup(id)
	if !ok || a.On == modaction.OnSession {
		return modaction.Action{}, false
	}
	return a, true
}

// Button is a notification's button as the consoles get it: the core
// action, its label (the core's), and the task it is about; or for
// question.answer the session, the question and the option.
type Button struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Project  string `json:"project,omitempty"`
	Task     int    `json:"task,omitempty"`
	Session  string `json:"session,omitempty"`
	Question string `json:"question,omitempty"`
	Option   string `json:"option,omitempty"`
}

// Target is b's target as the action binding takes it.
func (b Button) Target() map[string]string {
	if b.Session != "" {
		return map[string]string{"session": b.Session, "question": b.Question, "option": b.Option}
	}
	return map[string]string{"project": b.Project, "task": fmt.Sprintf("T%d", b.Task)}
}

// Notice is a notification in the tray.
type Notice struct {
	ID    string    `json:"id"`
	Mod   string    `json:"mod"`
	Title string    `json:"title"`
	Text  string    `json:"text,omitempty"`
	Level string    `json:"level"`
	At    time.Time `json:"at"`
	// Buttons are in the order the mod gave them.
	Buttons []Button `json:"buttons,omitempty"`
	// Drawn is set once a console has drawn the OS notification.
	Drawn bool `json:"drawn,omitempty"`
}

// Request is what a mod sent, checked by Hub.Post. Its buttons come with
// the project and the task as numbers, which the caller has parsed from the
// mod's target.
type Request struct {
	Title, Text, Level string
	Buttons            []Button
}

// Line makes s one line of at most n runes: control characters (escape
// sequences included) become spaces, runs of spaces one, and a cut ends in
// an ellipsis.
func Line(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}

// Errors Post answers with.
var (
	// ErrNoTray: there is no Hub (a bare Server in a test).
	ErrNoTray = errors.New("there is no notification tray")
	ErrRate   = errors.New("notifications are limited to " + fmt.Sprint(PerMinute) + " a minute")
)

// Hub is the tray (the methods of a nil Hub hold nothing and refuse): the notifications mods sent that are neither dismissed
// nor old.
type Hub struct {
	now func() time.Time

	mu   sync.Mutex
	next int
	list []*Notice
	sent map[string][]time.Time // per mod: when it sent, within the minute
}

// New makes a Hub; now is time.Now when nil.
func New(now func() time.Time) *Hub {
	if now == nil {
		now = time.Now
	}
	return &Hub{now: now, sent: map[string][]time.Time{}}
}

// Post checks req from mod and puts it in the tray. A mod over its rate is
// refused; one over its share loses its oldest.
func (h *Hub) Post(mod string, req Request) (Notice, error) {
	if h == nil {
		return Notice{}, ErrNoTray
	}
	title := Line(req.Title, MaxTitle)
	if title == "" {
		return Notice{}, errors.New("a notification needs a title")
	}
	level := req.Level
	if level == "" {
		level = "info"
	}
	ok := false
	for _, l := range Levels {
		ok = ok || l == level
	}
	if !ok {
		return Notice{}, fmt.Errorf("level must be one of %s", strings.Join(Levels, ", "))
	}
	if len(req.Buttons) > MaxButtons {
		return Notice{}, fmt.Errorf("a notification has at most %d buttons", MaxButtons)
	}
	n := Notice{Mod: mod, Title: title, Text: Line(req.Text, MaxText), Level: level}
	for _, b := range req.Buttons {
		a, ok := Action(b.ID)
		if !ok {
			return Notice{}, fmt.Errorf("%q is not an action a notification can name (%s)", b.ID, ActionIDs())
		}
		if a.On == modaction.OnQuestion {
			t := modaction.Target{Session: b.Session, Question: b.Question, Option: b.Option}
			if why := modaction.TargetWhy(a, t); why != "" || b.Project != "" || b.Task != 0 {
				return Notice{}, fmt.Errorf("%s needs a target with a session, a question and an option", b.ID)
			}
			if len(b.Session) > 256 || len(b.Question) > MaxQuestion || len(b.Option) > 256 ||
				strings.IndexFunc(b.Session+b.Question+b.Option, unicode.IsControl) >= 0 {
				return Notice{}, fmt.Errorf("%s: a session, question or option too long, or with control characters", b.ID)
			}
			n.Buttons = append(n.Buttons, Button{ID: a.ID, Label: a.Label, Session: b.Session, Question: b.Question, Option: b.Option})
			continue
		}
		if b.Project == "" || b.Task <= 0 || b.Session != "" {
			return Notice{}, fmt.Errorf("%s needs a target with a project and a task", b.ID)
		}
		n.Buttons = append(n.Buttons, Button{ID: a.ID, Label: a.Label, Project: b.Project, Task: b.Task})
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	h.expire(now)
	recent := h.sent[mod][:0:0]
	for _, t := range h.sent[mod] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= PerMinute {
		h.sent[mod] = recent
		return Notice{}, ErrRate
	}
	h.sent[mod] = append(recent, now)
	h.next++
	n.ID = fmt.Sprintf("n%d", h.next)
	n.At = now
	h.list = append(h.list, &n)
	h.trim(mod)
	return n, nil
}

// ActionIDs lists the action ids a button may name.
func ActionIDs() string {
	var ids []string
	for _, a := range modaction.Actions {
		if _, ok := Action(a.ID); ok {
			ids = append(ids, a.ID)
		}
	}
	return strings.Join(ids, ", ")
}

// trim drops mod's oldest beyond MaxKept and the oldest of all beyond
// MaxAll; h.mu held.
func (h *Hub) trim(mod string) {
	n := 0
	for _, x := range h.list {
		if x.Mod == mod {
			n++
		}
	}
	for i := 0; i < len(h.list) && n > MaxKept; {
		if h.list[i].Mod == mod {
			h.list = append(h.list[:i], h.list[i+1:]...)
			n--
			continue
		}
		i++
	}
	if len(h.list) > MaxAll {
		h.list = h.list[len(h.list)-MaxAll:]
	}
}

// expire drops the notifications older than TTL; h.mu held.
func (h *Hub) expire(now time.Time) {
	keep := h.list[:0]
	for _, x := range h.list {
		if now.Sub(x.At) < TTL {
			keep = append(keep, x)
		}
	}
	h.list = keep
}

// List is the tray, oldest first.
func (h *Hub) List() []Notice {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.now())
	out := make([]Notice, len(h.list))
	for i, x := range h.list {
		out[i] = *x
	}
	return out
}

// Claim marks notification id as drawn by the caller's console, and says
// whether this call was the first: only that console draws the OS
// notification, so several consoles don't ring it several times.
func (h *Hub) Claim(id string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, x := range h.list {
		if x.ID == id && !x.Drawn {
			x.Drawn = true
			return true
		}
	}
	return false
}

// Dismiss takes notification id out of the tray, and says whether it was
// there.
func (h *Hub) Dismiss(id string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, x := range h.list {
		if x.ID == id {
			h.list = append(h.list[:i], h.list[i+1:]...)
			return true
		}
	}
	return false
}

// DropMod takes all of mod's notifications out: it was disabled or removed.
func (h *Hub) DropMod(mod string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	keep := h.list[:0]
	for _, x := range h.list {
		if x.Mod != mod {
			keep = append(keep, x)
		}
	}
	h.list = keep
	delete(h.sent, mod)
}

// TaskNumber is the number of a task ref, "T12" or "12"; 0 when it isn't
// one.
func TaskNumber(ref string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(ref), "T"))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
