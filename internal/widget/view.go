package widget

import (
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/modaction"
)

// View is one widget tree on screen with the core's state: which list
// has the focus, each list's cursor and filter, and the scroll. Keys,
// clicks and the wheel move it here, so moving the cursor never needs a
// round trip to the mod host; only a key the mod declared, and a click on
// a row or a key hint, come back as an Event for it (ui.key, ui.click).
//
// A View is for one goroutine, the console's.
type View struct {
	tree  *Widget
	lists map[string]*list
	order []string // the lists and tables, in the tree's order
	keys  []Key    // every keys widget's, in order
	focus string
	// editing: keys type into the focused list's filter.
	editing bool
	top     int
	// follow: the next Render scrolls the cursor into view.
	follow bool
	// The last layout, for Render to reuse while nothing it shows
	// changed, and for clicks.
	lay      []line
	layW     int
	layValid bool
	shown    int // first laid-out line the last Render showed
	height   int // and how many lines it had

	// The action binding (mods design §11). bindKeys are the console's
	// keys for a bindable action; since is when each row's bindings,
	// by list and row id, last changed; shifted is when a publish last
	// moved the focus or the cursor to another row (not the human).
	bindKeys func(id string) []string
	since    map[rowKey]bound
	shifted  time.Time
	now      func() time.Time
}

// rowKey is a row of a list.
type rowKey struct{ list, row string }

// bound is a row's bindings as last published, and since when.
type bound struct {
	sig string
	at  time.Time
}

// list is a list's or table's state.
type list struct {
	w      *Widget
	cursor string // the row's id
	idx    int    // its place among the shown rows, for when it goes
	filter string
	shown  []int // the rows that pass the filter
}

// Event is what reaches the mod: a declared key pressed, or a click on a
// row or a key hint. Widget and Row are the focused (or clicked) list and
// the row under its cursor, if any.
//
// An EventAction never reaches the mod: it is a core action the row
// binds, pressed, for the console to re-check against live state, ask
// and run (the mods design §11). The mod hears of it only as action.done.
type Event struct {
	Kind   string `json:"kind"` // EventKey or EventClick
	Widget string `json:"widget,omitempty"`
	Row    string `json:"row,omitempty"`
	Key    string `json:"key,omitempty"`
	// Action is the bound action pressed (EventAction), and Stable
	// whether the row's bindings and the cursor's place had been
	// unchanged for modaction.Stable: a press that isn't counts for
	// nothing.
	Action *Action `json:"-"`
	Stable bool    `json:"-"`
}

// Event kinds.
const (
	EventKey    = "key"
	EventClick  = "click"
	EventAction = "action"
)

// NewView is a view with no tree yet.
func NewView() *View {
	return &View{lists: map[string]*list{}, since: map[rowKey]bound{}, now: time.Now, bindKeys: defaultKeys}
}

// defaultKeys are a bindable action's default keys.
func defaultKeys(id string) []string {
	if a, ok := modaction.Lookup(id); ok {
		return []string{a.Key}
	}
	return nil
}

// SetBindKeys gives the view the console's keys for each bindable
// action, its keymap's (the default key without one).
func (v *View) SetBindKeys(keys func(id string) []string) {
	if keys == nil {
		keys = defaultKeys
	}
	v.bindKeys, v.layValid = keys, false
}

// Tree is the tree on show, nil before Set.
func (v *View) Tree() *Widget { return v.tree }

// Set shows a newly published (and checked) tree. Each list keeps its
// filter and its cursor on the same row, by id; a cursor whose row went
// stays at its place. The focus stays on its list, else goes to the
// first one.
func (v *View) Set(t *Widget) {
	old := v.lists
	focus, cursor := v.focus, v.Cursor(v.focus)
	now := v.now()
	since := map[rowKey]bound{}
	v.tree, v.lists, v.order, v.keys = t, map[string]*list{}, nil, nil
	walk(t, func(w *Widget) {
		switch w.Kind {
		case KindList, KindTable:
			l := &list{w: w}
			if o := old[w.ID]; o != nil {
				l.cursor, l.idx, l.filter = o.cursor, o.idx, o.filter
			}
			l.refilter()
			v.lists[w.ID] = l
			v.order = append(v.order, w.ID)
			for i := range w.Rows {
				r := &w.Rows[i]
				if len(r.Actions) == 0 {
					continue
				}
				k, sig := rowKey{w.ID, r.ID}, bindSig(r.Actions)
				if b, ok := v.since[k]; ok && b.sig == sig {
					since[k] = b
				} else {
					since[k] = bound{sig, now}
				}
			}
		case KindKeys:
			v.keys = append(v.keys, w.Keys...)
		}
	})
	if v.lists[v.focus] == nil {
		v.focus, v.editing = "", false
		if len(v.order) > 0 {
			v.focus = v.order[0]
		}
	}
	v.since = since
	if v.focus != focus || v.Cursor(v.focus) != cursor {
		v.shifted = now
	}
	v.layValid = false
}

// bindSig is a row's bindings as one string, to tell when they change.
func bindSig(as []Action) string {
	var b strings.Builder
	for _, a := range as {
		t := ActionTarget(a)
		for _, s := range []string{a.ID, t.Project, t.Task, t.Thread, t.Session} {
			b.WriteString(s)
			b.WriteByte(0)
		}
	}
	return b.String()
}

func walk(w *Widget, f func(*Widget)) {
	if w == nil {
		return
	}
	f(w)
	for i := range w.Children {
		walk(&w.Children[i], f)
	}
}

// refilter works out the rows that pass the filter and puts the cursor
// back on its row, or at its place.
func (l *list) refilter() {
	l.shown = l.shown[:0]
	q := strings.ToLower(l.filter)
	for i := range l.w.Rows {
		if q == "" || strings.Contains(strings.ToLower(rowText(l.w, &l.w.Rows[i])), q) {
			l.shown = append(l.shown, i)
		}
	}
	if i := slices.IndexFunc(l.shown, func(r int) bool { return l.w.Rows[r].ID == l.cursor }); i >= 0 {
		l.idx = i
	}
	l.move(0)
}

// move moves the cursor by n shown rows, within them.
func (l *list) move(n int) {
	if len(l.shown) == 0 {
		l.idx, l.cursor = 0, ""
		return
	}
	l.idx = min(max(l.idx+n, 0), len(l.shown)-1)
	l.cursor = l.w.Rows[l.shown[l.idx]].ID
}

// rowText is a row's text, for the filter.
func rowText(w *Widget, r *Row) string {
	var b strings.Builder
	add := func(ss []Span) {
		for _, s := range ss {
			b.WriteString(s.Text)
			b.WriteByte(' ')
		}
	}
	add(r.Spans)
	for _, c := range r.Cells {
		add(c)
	}
	return b.String()
}

// Focus is the focused list's or table's id, "" for none.
func (v *View) Focus() string { return v.focus }

// Cursor is the id of the row under a list's cursor, "" for none.
func (v *View) Cursor(id string) string {
	if l := v.lists[id]; l != nil {
		return l.cursor
	}
	return ""
}

// Filter is a list's filter.
func (v *View) Filter(id string) string {
	if l := v.lists[id]; l != nil {
		return l.filter
	}
	return ""
}

// Editing tells whether keys type into the focused list's filter.
func (v *View) Editing() bool { return v.editing }

// Key handles a key, named as Bubble Tea names it ("down", "pgup", "/",
// "a"). It reports whether the view used it: one it didn't (esc with no
// filter, left, right, an undeclared key) is the frame's. A key the mod
// declared comes back as an event, and also counts as used.
func (v *View) Key(k string) (bool, *Event) {
	l := v.lists[v.focus]
	if v.editing && l != nil {
		return true, v.edit(l, k)
	}
	page := max(v.height-1, 1)
	switch k {
	case "up", "down", "pgup", "pgdown", "home", "end":
		n := map[string]int{"up": -1, "down": 1, "pgup": -page, "pgdown": page, "home": -1 << 30, "end": 1 << 30}[k]
		if l == nil {
			v.top += n
			v.top = max(v.top, 0) // the bottom is Render's to clamp
			return true, nil
		}
		l.move(n)
		v.follow = true
		return true, nil
	case "tab", "shift+tab":
		if len(v.order) < 2 {
			return len(v.order) == 1, nil
		}
		i := slices.Index(v.order, v.focus)
		if k == "tab" {
			i = (i + 1) % len(v.order)
		} else {
			i = (i + len(v.order) - 1) % len(v.order)
		}
		v.focus, v.follow, v.layValid = v.order[i], true, false
		return true, nil
	case "/":
		if l == nil {
			return false, nil
		}
		v.editing, v.follow, v.layValid = true, true, false
		return true, nil
	case "esc":
		if l == nil || l.filter == "" {
			return false, nil
		}
		v.setFilter(l, "")
		return true, nil
	}
	// A bound action's key is the core's: it runs the action the row
	// under the cursor binds, and never reaches the mod; the key of one
	// that changes state never does, even where the row binds none.
	if a := v.boundKey(k); a != nil {
		return true, v.press(a)
	}
	if v.coreKey(k) {
		return false, nil
	}
	if slices.ContainsFunc(v.keys, func(d Key) bool { return d.Key == k }) {
		return true, v.event(EventKey, k)
	}
	return false, nil
}

// Bound is a bound action on the row under the cursor, with the key
// that runs it.
type Bound struct {
	Key    string
	Action Action
}

// Bound are the actions the focused list's cursor row binds, each with
// the console's key for it, for the frame to show as hints in the
// core's words (modaction's labels). Where two share a key, the first
// is the one the key runs; the other isn't listed.
func (v *View) Bound() []Bound {
	r := v.cursorRow()
	if r == nil {
		return nil
	}
	var out []Bound
	for _, a := range r.Actions {
		for _, k := range v.bindKeys(a.ID) {
			if !slices.ContainsFunc(out, func(b Bound) bool { return b.Key == k }) {
				out = append(out, Bound{k, a})
				break
			}
		}
	}
	return out
}

// Press presses bound action id on the row under the cursor, as a click
// on its hint does: nil when the row doesn't bind it.
func (v *View) Press(id string) *Event {
	r := v.cursorRow()
	if r == nil {
		return nil
	}
	for i := range r.Actions {
		if r.Actions[i].ID == id {
			return v.press(&r.Actions[i])
		}
	}
	return nil
}

// boundKey is the action key k runs on the row under the cursor, nil for
// none.
func (v *View) boundKey(k string) *Action {
	for _, b := range v.Bound() {
		if b.Key == k {
			return &b.Action
		}
	}
	return nil
}

// coreKey says whether k is a key of a bindable action that changes
// state (one that asks first): the core's in a view, on every row, so a
// mod's keys widget can't put its own label on D, A or x. A navigation's
// key (enter) stays the mod's on a row that binds none.
func (v *View) coreKey(k string) bool {
	for _, a := range modaction.Actions {
		if a.Confirm && slices.Contains(v.bindKeys(a.ID), k) {
			return true
		}
	}
	return false
}

// cursorRow is the focused list's row under its cursor, nil for none.
func (v *View) cursorRow() *Row {
	l := v.lists[v.focus]
	if l == nil || l.cursor == "" {
		return nil
	}
	for i := range l.w.Rows {
		if l.w.Rows[i].ID == l.cursor {
			return &l.w.Rows[i]
		}
	}
	return nil
}

// press is the event of bound action a pressed on the cursor's row: it
// counts only once the row's bindings, and the row the cursor is on,
// had stayed as they are for modaction.Stable.
func (v *View) press(a *Action) *Event {
	e := v.event(EventAction, "")
	cp := *a
	e.Action = &cp
	b, ok := v.since[rowKey{e.Widget, e.Row}]
	now := v.now()
	e.Stable = ok && now.Sub(b.at) >= modaction.Stable && now.Sub(v.shifted) >= modaction.Stable
	return e
}

// edit types into the filter: enter keeps it, esc drops it, the arrows
// still move.
func (v *View) edit(l *list, k string) *Event {
	switch k {
	case "enter":
		v.editing, v.layValid = false, false
	case "esc":
		v.editing = false
		v.setFilter(l, "")
	case "backspace":
		if r := []rune(l.filter); len(r) > 0 {
			v.setFilter(l, string(r[:len(r)-1]))
		}
	case "up", "down", "pgup", "pgdown", "home", "end":
		v.editing = false
		v.Key(k)
		v.editing = true
	case "space":
		v.setFilter(l, l.filter+" ")
	default:
		r := []rune(k)
		if len(r) == 1 && r[0] >= ' ' && len(l.filter) < 64 {
			if c := Clean(k); c != "" {
				if _, wide := wideGlyph(c); !wide {
					v.setFilter(l, l.filter+c)
				}
			}
		}
	}
	return nil
}

func (v *View) setFilter(l *list, f string) {
	l.filter = f
	l.refilter()
	v.follow, v.layValid = true, false
}

func (v *View) event(kind, key string) *Event {
	e := &Event{Kind: kind, Key: key, Widget: v.focus}
	if l := v.lists[v.focus]; l != nil {
		e.Row = l.cursor
	}
	return e
}

// Wheel scrolls by n lines (up when negative) without moving the cursor.
func (v *View) Wheel(n int) {
	v.top = max(v.top+n, 0)
	v.follow = false
}

// Click handles a click at cell x of line y of the last Render. A row
// takes the focus and the cursor and comes back as a click event; a key
// hint comes back as its key's event; elsewhere nothing happens.
func (v *View) Click(x, y int) *Event {
	if y < 0 || y >= v.height || v.shown+y >= len(v.lay) {
		return nil
	}
	ln := &v.lay[v.shown+y]
	if ln.list != "" {
		l := v.lists[ln.list]
		if l == nil {
			return nil
		}
		if i := slices.IndexFunc(l.shown, func(r int) bool { return l.w.Rows[r].ID == ln.row }); i >= 0 {
			l.idx, l.cursor = i, ln.row
		}
		if v.focus != ln.list {
			v.focus, v.editing, v.layValid = ln.list, false, false
		}
		return v.event(EventClick, "")
	}
	for _, h := range ln.keys {
		if x >= h.x0 && x < h.x1 {
			return v.event(EventKey, h.key)
		}
	}
	return nil
}
