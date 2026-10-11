package widget

import (
	"slices"
	"strings"
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
type Event struct {
	Kind   string `json:"kind"` // EventKey or EventClick
	Widget string `json:"widget,omitempty"`
	Row    string `json:"row,omitempty"`
	Key    string `json:"key,omitempty"`
}

// Event kinds.
const (
	EventKey   = "key"
	EventClick = "click"
)

// NewView is a view with no tree yet.
func NewView() *View { return &View{lists: map[string]*list{}} }

// Tree is the tree on show, nil before Set.
func (v *View) Tree() *Widget { return v.tree }

// Set shows a newly published (and checked) tree. Each list keeps its
// filter and its cursor on the same row, by id; a cursor whose row went
// stays at its place. The focus stays on its list, else goes to the
// first one.
func (v *View) Set(t *Widget) {
	old := v.lists
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
	v.layValid = false
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
	if slices.ContainsFunc(v.keys, func(d Key) bool { return d.Key == k }) {
		return true, v.event(EventKey, k)
	}
	return false, nil
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
