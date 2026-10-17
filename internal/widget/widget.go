// Package widget is the widget set a mod draws with (mods design §6): a
// small tree of text, list, table, kv, progress, steps, bars and keys,
// stacked, that the mod publishes as JSON and terminatr draws with its own
// theme, icons, widths, scrolling and mouse hits. A mod never writes
// terminal bytes: span text has its control characters stripped and its
// wide glyphs replaced, and a span's look is a theme role or an icon name.
//
// The list state is the core's (State): the cursor, the scroll and the
// filter move on a key or a click without a round trip to the mod host.
// Only the keys the mod declared in a keys widget, and clicks on rows,
// reach it, as events naming the widget and the row.
package widget

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/theclifmeister/terminatr/internal/age"
	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The kinds of widget.
const (
	KindStack    = "stack"    // Children, top to bottom
	KindText     = "text"     // Text (wrapped) or Lines of spans
	KindList     = "list"     // selectable Rows of spans
	KindTable    = "table"    // Columns and selectable Rows of cells
	KindKV       = "kv"       // Pairs: a key and its value's spans
	KindProgress = "progress" // Value out of Max, with a Label
	KindSteps    = "steps"    // Steps, each done, now or open
	KindBars     = "bars"     // Values as a bar chart, or a sparkline
	KindKeys     = "keys"     // Keys the mod handles, drawn as hints
)

// Kinds are the widget kinds, as the API names them.
var Kinds = []string{KindStack, KindText, KindList, KindTable, KindKV, KindProgress, KindSteps, KindBars, KindKeys}

// Roles are the theme roles a span may name (mods design §6, Spans).
var Roles = []string{"accent", "good", "warn", "bad", "info", "faint", "head"}

// Icons are the icon names a span may name: each is drawn from the
// console's icon set (nerd, unicode or ascii).
var Icons = []string{
	"working", "blocked", "idle", "starting", "running", "review", "done", "other",
	"ci.pass", "ci.fail", "ci.run",
	"todo.done", "todo.now", "todo.open",
	"hint", "remote", "paused", "inactive", "folder", "thread", "coordinator",
}

// Widget is one node of the tree. Kind picks which of the other fields
// count; the rest are ignored.
type Widget struct {
	Kind string `json:"kind"`
	// ID names a list, table or keys widget in the events it sends, and
	// keys the list's state across publishes. A list or table needs one.
	ID string `json:"id,omitempty"`
	// Title is a head line drawn above the widget.
	Title string `json:"title,omitempty"`

	Children []Widget `json:"children,omitempty"` // stack

	Text  string   `json:"text,omitempty"`  // text: wrapped to the width
	Role  string   `json:"role,omitempty"`  // text, progress: the role it is drawn in
	Lines [][]Span `json:"lines,omitempty"` // text: lines of spans, clipped

	Columns []Column `json:"columns,omitempty"` // table
	Rows    []Row    `json:"rows,omitempty"`    // list, table
	Empty   string   `json:"empty,omitempty"`   // list, table: shown with no rows

	Pairs []Pair `json:"pairs,omitempty"` // kv

	Label string  `json:"label,omitempty"` // progress
	Value float64 `json:"value,omitempty"` // progress
	Max   float64 `json:"max,omitempty"`   // progress; 0 is 100

	Steps []Step `json:"steps,omitempty"` // steps

	Values []float64 `json:"values,omitempty"` // bars
	Labels []string  `json:"labels,omitempty"` // bars: one per value, for a chart
	Spark  bool      `json:"spark,omitempty"`  // bars: one line, ▁▂▃▅▇

	Keys []Key `json:"keys,omitempty"` // keys
	// Actions are core actions bound in the whole view, whatever row
	// the cursor is on (keys; mod API 1.4), such as task.new: an action
	// no row stands for. A row's own action on the same key comes first.
	Actions []Action `json:"actions,omitempty"`
}

// Span is a piece of text in a role, or an icon (mods design §6):
// [{text: "3d", role: "warn"}, {icon: "review"}].
//
// Ago and Until (mod API 1.7) are times, RFC 3339, that the console
// draws with its own clock in place of Text, so they move without a new
// publish: Ago as "40s ago", Until as "1m20s", or "due" once it passed
// (Words). A span has at most one of Text, Ago and Until.
type Span struct {
	Text  string `json:"text,omitempty"`
	Role  string `json:"role,omitempty"`
	Icon  string `json:"icon,omitempty"`
	Ago   string `json:"ago,omitempty"`
	Until string `json:"until,omitempty"`
}

// Timed tells whether the span is drawn from the clock (Ago or Until).
func (s Span) Timed() bool { return s.Ago != "" || s.Until != "" }

// Words is the span's text as drawn at now: Text, or its Ago or Until
// in words (age.Words). A time that doesn't parse (a span not checked)
// draws as nothing.
func (s Span) Words(now time.Time) string {
	switch {
	case s.Ago != "":
		t, err := time.Parse(time.RFC3339, s.Ago)
		if err != nil {
			return ""
		}
		return age.Words(now.Sub(t)) + " ago"
	case s.Until != "":
		t, err := time.Parse(time.RFC3339, s.Until)
		if err != nil {
			return ""
		}
		if d := t.Sub(now); d >= time.Second {
			return age.Words(d)
		}
		return "due"
	}
	return s.Text
}

// Timed tells whether a span anywhere in the tree is drawn from the
// clock (Span.Timed): its drawing then changes every second.
func (w *Widget) Timed() bool {
	timed := func(ss []Span) bool { return slices.ContainsFunc(ss, Span.Timed) }
	for _, ss := range w.Lines {
		if timed(ss) {
			return true
		}
	}
	for _, p := range w.Pairs {
		if timed(p.Value) {
			return true
		}
	}
	for i := range w.Rows {
		r := &w.Rows[i]
		if timed(r.Spans) || slices.ContainsFunc(r.Cells, timed) || slices.ContainsFunc(r.More, timed) {
			return true
		}
	}
	for i := range w.Children {
		if w.Children[i].Timed() {
			return true
		}
	}
	return false
}

// Row is a list's or table's row. Spans are a list row's; Cells are a
// table row's, one per column. ID keeps the cursor on the row across
// publishes and names it in events; it defaults to the row's index.
type Row struct {
	ID    string   `json:"id,omitempty"`
	Spans []Span   `json:"spans,omitempty"`
	Cells [][]Span `json:"cells,omitempty"`
	// Actions name the core actions the row's keys run (mods design §11).
	// The widget only carries them: the core's key runs one, after the
	// console re-checks it against live state and asks (View.Key), never
	// the mod.
	Actions []Action `json:"actions,omitempty"`
	// Head makes the row a section heading (mod API 1.2): its spans
	// (a table's too) drawn as a rule across the width, after a blank
	// line unless it comes first. The cursor never stops on one, the
	// filter hides it, and it binds nothing.
	Head bool `json:"head,omitempty"`
	// More are lines drawn under the row while the cursor is on it
	// (mod API 1.2), such as a task's steps: the console shows them as
	// the cursor moves, without asking the mod.
	More [][]Span `json:"more,omitempty"`
}

// Action is a core action id and its target, such as task.delegate on
// {project: "alpha", task: "T12"}.
type Action struct {
	ID     string            `json:"id"`
	Target map[string]string `json:"target,omitempty"`
}

// Column is a table column. Width 0 sizes it to its cells.
type Column struct {
	Title string `json:"title"`
	Width int    `json:"width,omitempty"`
	Right bool   `json:"right,omitempty"` // align right, for numbers
}

// Pair is one kv line.
type Pair struct {
	Key   string `json:"key"`
	Value []Span `json:"value"`
}

// Step states.
const (
	StepDone = "done"
	StepNow  = "now"
	StepOpen = "open"
)

// Step is one line of a steps widget. State is done, now or open (the
// default).
type Step struct {
	Text  string `json:"text"`
	State string `json:"state,omitempty"`
}

// Key is a key the mod handles: pressed in the view, it reaches the mod
// as ui.key with the focused list and row.
type Key struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Limits on a tree, so a mod can't make a frame slow to draw.
const (
	MaxBytes    = 1 << 20 // the JSON
	MaxDepth    = 4       // stacks in stacks
	MaxWidgets  = 256
	MaxRows     = 5000 // in all the lists and tables
	MaxSpans    = 16   // in a row, a cell, a line or a value
	MaxColumns  = 12
	MaxValues   = 512
	MaxKeys     = 12
	MaxActions  = 8        // bound on one row
	MaxMore     = 64       // lines under one row
	MaxText     = 4096     // bytes of one string
	MaxBody     = 64 << 10 // bytes of a text widget's text, a long file's
	MaxTextLine = 2000     // lines of a text widget, pairs, steps
)

var idRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// actionRE is a core action id's form: dotted lower-case words.
var actionRE = regexp.MustCompile(`^[a-z]+(\.[a-z]+(-[a-z]+)*)+$`)

// ReservedKeys are the core's in a widget view: moving, scrolling,
// filtering, focus, closing, the popup's tabs, and the keys tm keeps for
// itself everywhere. A keys widget can't declare one.
var ReservedKeys = []string{
	"up", "down", "left", "right", "pgup", "pgdown", "home", "end",
	"esc", "tab", "shift+tab", "/", "backspace", "ctrl+c",
	"d", "q", "r", "1", "2", "3", "4", "5", "6", "7", "8", "9",
}

// Decode reads a published tree and checks it (Check).
func Decode(data []byte) (*Widget, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("widget tree of %d bytes is over the %d limit", len(data), MaxBytes)
	}
	var w Widget
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("widget tree: %w", err)
	}
	if err := w.Check(); err != nil {
		return nil, err
	}
	return &w, nil
}

// Check checks the tree in place: kinds, roles, icons, ids, keys and the
// limits. It strips control and format characters from every string and
// replaces each wide glyph (an emoji, a CJK character), whose width
// differs between terminals, with a ?. The error names the node, as
// children[1].rows[3].spans[0].
func (w *Widget) Check() error {
	c := checker{ids: map[string]bool{}}
	c.widget(w, "", 0)
	return errors.Join(c.errs...)
}

// CheckSpans checks spans that stand alone, a cell or a status segment
// (mods design §6, Spans), as Check checks a tree's: at most MaxSpans,
// roles and icons by name, each text cleaned in place, newlines become
// spaces, wide glyphs ?. The error names the span, as spans[2].
func CheckSpans(ss []Span) error {
	c := checker{ids: map[string]bool{}}
	c.spans(ss, "spans")
	return errors.Join(c.errs...)
}

type checker struct {
	errs          []error
	ids           map[string]bool
	widgets, rows int
}

func (c *checker) bad(at, format string, a ...any) {
	if len(c.errs) < 10 {
		if at == "" {
			at = "widget"
		}
		c.errs = append(c.errs, fmt.Errorf("%s: %s", at, fmt.Sprintf(format, a...)))
	}
}

func join(at, field string) string {
	if at == "" {
		return field
	}
	return at + "." + field
}

func (c *checker) widget(w *Widget, at string, depth int) {
	c.widgets++
	if c.widgets == MaxWidgets+1 {
		c.bad(at, "more than %d widgets", MaxWidgets)
	}
	if !slices.Contains(Kinds, w.Kind) {
		c.bad(at, "kind %q is not one of %s", w.Kind, strings.Join(Kinds, ", "))
		return
	}
	c.str(&w.Title, join(at, "title"))
	if w.ID == "" && (w.Kind == KindList || w.Kind == KindTable) {
		c.bad(at, "a %s needs an id", w.Kind)
	}
	if w.ID != "" {
		if !idRE.MatchString(w.ID) {
			c.bad(join(at, "id"), "%q is not an id (letters, digits, . _ -, at most 64)", w.ID)
		} else if c.ids[w.ID] {
			c.bad(join(at, "id"), "%q is used twice", w.ID)
		}
		c.ids[w.ID] = true
	}
	switch w.Kind {
	case KindStack:
		if depth >= MaxDepth {
			c.bad(at, "stacks nest at most %d deep", MaxDepth)
			return
		}
		for i := range w.Children {
			c.widget(&w.Children[i], fmt.Sprintf("%s[%d]", join(at, "children"), i), depth+1)
		}
	case KindText:
		c.body(&w.Text, join(at, "text"))
		c.role(w.Role, join(at, "role"))
		if n := strings.Count(w.Text, "\n") + len(w.Lines); n > MaxTextLine {
			c.bad(at, "%d lines, over %d", n, MaxTextLine)
		}
		for i := range w.Lines {
			c.spans(w.Lines[i], fmt.Sprintf("%s[%d]", join(at, "lines"), i))
		}
	case KindList, KindTable:
		c.str(&w.Empty, join(at, "empty"))
		if w.Kind == KindTable {
			c.columns(w, at)
		}
		c.rowsOf(w, at)
	case KindKV:
		c.pairs(w, at)
	case KindProgress:
		c.str(&w.Label, join(at, "label"))
		c.role(w.Role, join(at, "role"))
		if w.Max < 0 || w.Value < 0 {
			c.bad(at, "value and max can't be negative")
		}
	case KindSteps:
		c.steps(w, at)
	case KindBars:
		c.bars(w, at)
	case KindKeys:
		c.keys(w, at)
		c.actions(w.Actions, join(at, "actions"))
	}
	if len(w.Actions) > 0 && w.Kind != KindKeys {
		c.bad(join(at, "actions"), "only a keys widget, or a row, binds actions")
	}
}

func (c *checker) columns(w *Widget, at string) {
	if len(w.Columns) == 0 || len(w.Columns) > MaxColumns {
		c.bad(join(at, "columns"), "a table has 1 to %d columns, not %d", MaxColumns, len(w.Columns))
	}
	for i := range w.Columns {
		col := &w.Columns[i]
		c.str(&col.Title, fmt.Sprintf("%s[%d].title", join(at, "columns"), i))
		if col.Width < 0 || col.Width > 200 {
			c.bad(fmt.Sprintf("%s[%d].width", join(at, "columns"), i), "%d is not 0 to 200", col.Width)
		}
	}
}

func (c *checker) pairs(w *Widget, at string) {
	for i := range w.Pairs {
		p := &w.Pairs[i]
		pat := fmt.Sprintf("%s[%d]", join(at, "pairs"), i)
		c.str(&p.Key, join(pat, "key"))
		c.spans(p.Value, join(pat, "value"))
	}
	if len(w.Pairs) > MaxTextLine {
		c.bad(at, "%d pairs, over %d", len(w.Pairs), MaxTextLine)
	}
}

func (c *checker) steps(w *Widget, at string) {
	for i := range w.Steps {
		s := &w.Steps[i]
		sat := fmt.Sprintf("%s[%d]", join(at, "steps"), i)
		c.str(&s.Text, join(sat, "text"))
		if s.State != "" && s.State != StepDone && s.State != StepNow && s.State != StepOpen {
			c.bad(join(sat, "state"), "%q is not done, now or open", s.State)
		}
	}
	if len(w.Steps) > MaxTextLine {
		c.bad(at, "%d steps, over %d", len(w.Steps), MaxTextLine)
	}
}

func (c *checker) bars(w *Widget, at string) {
	if len(w.Values) > MaxValues {
		c.bad(join(at, "values"), "%d values, over %d", len(w.Values), MaxValues)
	}
	for i, v := range w.Values {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			c.bad(fmt.Sprintf("%s[%d]", join(at, "values"), i), "%v is not a number from 0", v)
		}
	}
	if len(w.Labels) > 0 && len(w.Labels) != len(w.Values) {
		c.bad(join(at, "labels"), "%d labels for %d values", len(w.Labels), len(w.Values))
	}
	for i := range w.Labels {
		c.str(&w.Labels[i], fmt.Sprintf("%s[%d]", join(at, "labels"), i))
	}
}

func (c *checker) keys(w *Widget, at string) {
	if len(w.Keys) > MaxKeys {
		c.bad(join(at, "keys"), "%d keys, over %d", len(w.Keys), MaxKeys)
	}
	seen := map[string]bool{}
	for i := range w.Keys {
		k := &w.Keys[i]
		kat := fmt.Sprintf("%s[%d]", join(at, "keys"), i)
		c.str(&k.Label, join(kat, "label"))
		switch {
		case !validKey(k.Key):
			c.bad(join(kat, "key"), "%q is not a key (one printable character, or enter)", k.Key)
		case slices.Contains(ReservedKeys, k.Key):
			c.bad(join(kat, "key"), "%q is terminatr's in a view", k.Key)
		case seen[k.Key]:
			c.bad(join(kat, "key"), "%q is declared twice", k.Key)
		}
		seen[k.Key] = true
	}
}

func (c *checker) rowsOf(w *Widget, at string) {
	ids := map[string]bool{}
	for i := range w.Rows {
		c.rows++
		if c.rows == MaxRows+1 {
			c.bad(at, "more than %d rows in the tree", MaxRows)
		}
		r := &w.Rows[i]
		rat := fmt.Sprintf("%s[%d]", join(at, "rows"), i)
		if r.ID == "" {
			r.ID = fmt.Sprintf("#%d", i)
		} else if len(r.ID) > 256 || strings.IndexFunc(r.ID, unicode.IsControl) >= 0 {
			c.bad(join(rat, "id"), "not an id (at most 256 bytes, no control characters)")
		}
		if ids[r.ID] {
			c.bad(join(rat, "id"), "%q is used twice", r.ID)
		}
		ids[r.ID] = true
		if w.Kind == KindTable {
			if len(r.Cells) > len(w.Columns) {
				c.bad(join(rat, "cells"), "%d cells for %d columns", len(r.Cells), len(w.Columns))
			}
			for j := range r.Cells {
				c.spans(r.Cells[j], fmt.Sprintf("%s[%d]", join(rat, "cells"), j))
			}
		} else {
			c.spans(r.Spans, join(rat, "spans"))
		}
		if r.Head {
			if w.Kind == KindTable {
				c.spans(r.Spans, join(rat, "spans"))
			}
			if len(r.Cells) > 0 || len(r.Actions) > 0 || len(r.More) > 0 {
				c.bad(rat, "a head row has spans only: no cells, actions or more")
			}
		}
		if len(r.More) > MaxMore {
			c.bad(join(rat, "more"), "%d lines, over %d", len(r.More), MaxMore)
		}
		for j := range r.More {
			c.spans(r.More[j], fmt.Sprintf("%s[%d]", join(rat, "more"), j))
		}
		c.actions(r.Actions, join(rat, "actions"))
	}
}

// actions checks a row's or a keys widget's bound actions (mods design §11): each names a
// core action a mod may bind (modaction), once, on a target of the shape
// it takes. Whether it is available is checked when it is pressed.
func (c *checker) actions(as []Action, at string) {
	if len(as) > MaxActions {
		c.bad(at, "%d actions, over %d", len(as), MaxActions)
	}
	seen := map[string]bool{}
	for j, a := range as {
		aat := fmt.Sprintf("%s[%d]", at, j)
		if !actionRE.MatchString(a.ID) {
			c.bad(join(aat, "id"), "%q is not an action id", a.ID)
			continue
		}
		spec, ok := modaction.Lookup(a.ID)
		switch {
		case !ok:
			c.bad(join(aat, "id"), "%q is not an action a mod may bind ($.actions.list names them)", a.ID)
			continue
		case seen[a.ID]:
			c.bad(join(aat, "id"), "%q is bound twice", a.ID)
		}
		seen[a.ID] = true
		for k, v := range a.Target {
			if !slices.Contains(targetKeys, k) {
				c.bad(join(aat, "target"), "%q is not a target field (%s)", k, strings.Join(targetKeys, ", "))
			} else if k == "question" && len(v) > MaxQuestionTarget || k != "question" && len(v) > 256 || strings.IndexFunc(v, unicode.IsControl) >= 0 {
				c.bad(join(aat, "target."+k), "not an id (at most 256 bytes, a question %d, no control characters)", MaxQuestionTarget)
			}
		}
		if why := modaction.TargetWhy(spec, ActionTarget(a)); why != "" {
			c.bad(join(aat, "target"), "%s", why)
		}
	}
}

// MaxQuestionTarget bounds a question.answer target's question text.
const MaxQuestionTarget = 1024

// targetKeys are a target's fields, mod API v1's (modbus.Target).
var targetKeys = []string{"project", "task", "thread", "session", "question", "option", "path"}

// ActionTarget is a's target as the core action table takes it.
func ActionTarget(a Action) modaction.Target { return modaction.TargetOf(a.Target) }

func (c *checker) spans(ss []Span, at string) {
	if len(ss) > MaxSpans {
		c.bad(at, "%d spans, over %d", len(ss), MaxSpans)
	}
	for i := range ss {
		s := &ss[i]
		sat := fmt.Sprintf("%s[%d]", at, i)
		c.str(&s.Text, join(sat, "text"))
		c.role(s.Role, join(sat, "role"))
		if s.Icon != "" && !slices.Contains(Icons, s.Icon) {
			c.bad(join(sat, "icon"), "%q is not an icon name", s.Icon)
		}
		c.when(&s.Ago, join(sat, "ago"))
		c.when(&s.Until, join(sat, "until"))
		if n := btoi(s.Text != "") + btoi(s.Ago != "") + btoi(s.Until != ""); n > 1 {
			c.bad(sat, "has %d of text, ago and until; at most one", n)
		}
	}
}

// when checks a span's Ago or Until: a time in RFC 3339.
func (c *checker) when(s *string, at string) {
	if *s == "" {
		return
	}
	if _, err := time.Parse(time.RFC3339, *s); err != nil {
		c.bad(at, "not a time in RFC 3339, as 2026-10-13T14:05:00Z")
		*s = ""
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (c *checker) role(r, at string) {
	if r != "" && !slices.Contains(Roles, r) {
		c.bad(at, "role %q is not one of %s", r, strings.Join(Roles, ", "))
	}
}

// str cleans *s in place (Clean, a newline becoming a space, Narrow)
// and refuses long text.
func (c *checker) str(s *string, at string) {
	c.text(s, at, MaxText)
	*s = strings.ReplaceAll(*s, "\n", " ")
}

// body is str keeping newlines, for a text widget's Text, up to MaxBody.
func (c *checker) body(s *string, at string) { c.text(s, at, MaxBody) }

func (c *checker) text(s *string, at string, limit int) {
	if len(*s) > limit {
		c.bad(at, "%d bytes, over %d", len(*s), limit)
		*s = (*s)[:0]
		return
	}
	*s = Narrow(Clean(*s))
}

// Clean replaces what could reach the terminal as other than text with
// spaces (textsafe.Clean). A newline stays, for a text widget to break
// on.
func Clean(s string) string { return textsafe.Clean(s, true) }

// Narrow replaces each grapheme of s wider than one cell, an emoji or a
// CJK character, with a ?: terminals disagree on how wide they draw, so
// the rest of the line would shift.
func Narrow(s string) string {
	if _, ok := wideGlyph(s); !ok {
		return s
	}
	var b strings.Builder
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		if g.Width() > 1 {
			b.WriteByte('?')
		} else {
			b.WriteString(g.Str())
		}
	}
	return b.String()
}

// wideGlyph is the first grapheme of s wider than one cell. Below
// U+1100 nothing is wide but with a variation selector, which is above.
func wideGlyph(s string) (string, bool) {
	if strings.IndexFunc(s, func(r rune) bool { return r >= 0x1100 }) < 0 {
		return "", false
	}
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		if g.Width() > 1 {
			return g.Str(), true
		}
	}
	return "", false
}

// validKey is one printable ASCII character other than space, or enter.
func validKey(k string) bool {
	return k == "enter" || len(k) == 1 && k[0] > ' ' && k[0] < 0x7f
}
