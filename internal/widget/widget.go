// Package widget is the widget set a mod draws with (mods design §6): a
// small tree of text, list, table, kv, progress, steps, bars and keys,
// stacked, that the mod publishes as JSON and terminatr draws with its own
// theme, icons, widths, scrolling and mouse hits. A mod never writes
// terminal bytes: span text has its control characters stripped and
// refuses wide glyphs, and a span's look is a theme role or an icon name.
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
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/theclifmeister/terminatr/internal/modaction"
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
}

// Span is a piece of text in a role, or an icon (mods design §6):
// [{text: "3d", role: "warn"}, {icon: "review"}].
type Span struct {
	Text string `json:"text,omitempty"`
	Role string `json:"role,omitempty"`
	Icon string `json:"icon,omitempty"`
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
	MaxWidgets  = 64
	MaxRows     = 5000 // in all the lists and tables
	MaxSpans    = 16   // in a row, a cell, a line or a value
	MaxColumns  = 12
	MaxValues   = 512
	MaxKeys     = 12
	MaxActions  = 8    // bound on one row
	MaxText     = 4096 // bytes of one string
	MaxTextLine = 500  // lines of a text widget
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
// refuses a string with a wide glyph (an emoji, a CJK character), whose
// width differs between terminals. The error names the node, as
// children[1].rows[3].spans[0].
func (w *Widget) Check() error {
	c := checker{ids: map[string]bool{}}
	c.widget(w, "", 0)
	return errors.Join(c.errs...)
}

// CheckSpans checks spans that stand alone, a cell or a status segment
// (mods design §6, Spans), as Check checks a tree's: at most MaxSpans,
// roles and icons by name, each text cleaned in place, newlines become
// spaces, wide glyphs refused. The error names the span, as spans[2].
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
		c.text(&w.Text, join(at, "text"))
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
		c.actions(r.Actions, join(rat, "actions"))
	}
}

// actions checks a row's bound actions (mods design §11): each names a
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
			} else if len(v) > 256 || strings.IndexFunc(v, unicode.IsControl) >= 0 {
				c.bad(join(aat, "target."+k), "not an id (at most 256 bytes, no control characters)")
			}
		}
		if why := modaction.TargetWhy(spec, ActionTarget(a)); why != "" {
			c.bad(join(aat, "target"), "%s", why)
		}
	}
}

// targetKeys are a target's fields, mod API v1's (modbus.Target).
var targetKeys = []string{"project", "task", "thread", "session"}

// ActionTarget is a's target as the core action table takes it.
func ActionTarget(a Action) modaction.Target {
	return modaction.Target{Project: a.Target["project"], Task: a.Target["task"], Thread: a.Target["thread"], Session: a.Target["session"]}
}

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
	}
}

func (c *checker) role(r, at string) {
	if r != "" && !slices.Contains(Roles, r) {
		c.bad(at, "role %q is not one of %s", r, strings.Join(Roles, ", "))
	}
}

// str cleans *s in place (Clean, a newline becoming a space) and refuses
// wide glyphs and long text.
func (c *checker) str(s *string, at string) {
	c.text(s, at)
	*s = strings.ReplaceAll(*s, "\n", " ")
}

// text is str keeping newlines, for a text widget's Text.
func (c *checker) text(s *string, at string) {
	if len(*s) > MaxText {
		c.bad(at, "%d bytes, over %d", len(*s), MaxText)
		*s = (*s)[:0]
		return
	}
	*s = Clean(*s)
	if g, ok := wideGlyph(*s); ok {
		c.bad(at, "%q is a wide glyph (emoji and wide characters are refused)", g)
	}
}

// Clean strips what could reach the terminal as other than text: control
// characters (escape sequences start with one) and Unicode format
// characters (bidi overrides, zero-width joiners). A newline stays, for a
// text widget to break on; a tab becomes a space.
func Clean(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) }) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r == '\n':
			b.WriteRune(r)
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// wideGlyph is the first grapheme of s wider than one cell.
func wideGlyph(s string) (string, bool) {
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
