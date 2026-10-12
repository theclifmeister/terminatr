package widget

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modaction"
)

// TestCheckActions: a row binds only the core's bindable actions, once
// each, on targets of their shape.
func TestCheckActions(t *testing.T) {
	row := func(actions string) string {
		return `{"kind":"list","id":"a","rows":[{"id":"T1","actions":` + actions + `}]}`
	}
	for _, tc := range []struct{ js, want string }{
		{row(`[{"id":"settings.open"}]`), "not an action a mod may bind"},
		{row(`[{"id":"thread.merge","target":{"project":"p"}}]`), "not an action a mod may bind"},
		{row(`[{"id":"task.accept","target":{"project":"p","task":"T1"}},{"id":"task.accept","target":{"project":"p","task":"T2"}}]`), "bound twice"},
		{row(`[{"id":"task.accept","target":{"project":"p"}}]`), "takes a target of"},
		{row(`[{"id":"task.accept","target":{"project":"p","task":"T1","url":"x"}}]`), `"url" is not a target field`},
		{row(`[{"id":"session.attach","target":{"session":"s\u001b1"}}]`), "no control characters"},
	} {
		_, err := Decode([]byte(tc.js))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.js, err, tc.want)
		}
	}
	if _, err := Decode([]byte(row(`[{"id":"task.accept","target":{"project":"p","task":"T1"}},{"id":"task.send-back","target":{"project":"p","task":"T1"}}]`))); err != nil {
		t.Errorf("a good row: %v", err)
	}
}

// clockView is a view on a clock the test moves.
func clockView(t *testing.T) (*View, *time.Time) {
	now := time.Unix(1000, 0)
	v := NewView()
	v.now = func() time.Time { return now }
	return v, &now
}

const bindTree = `{"kind":"stack","children":[
	{"kind":"list","id":"tasks","rows":[
		{"id":"T1","spans":[{"text":"T1 one"}],"actions":[{"id":"task.accept","target":{"project":"p","task":"T1"}},{"id":"task.show","target":{"project":"p","task":"T1"}}]},
		{"id":"T2","spans":[{"text":"T2 two"}],"actions":[{"id":"task.delegate","target":{"project":"p","task":"T2"}}]},
		{"id":"T3","spans":[{"text":"T3 three"}]}]},
	{"kind":"keys","id":"k","keys":[{"key":"D","label":"view"},{"key":"o","label":"open"}]}]}`

// TestBoundKeys: the core's key runs the row's bound action as an
// EventAction (never the mod's ui.key); the key of an action that writes
// is never the mod's, and its label is not drawn.
func TestBoundKeys(t *testing.T) {
	v, now := clockView(t)
	v.Set(mustDecode(t, bindTree))
	*now = now.Add(time.Second)

	ok, ev := v.Key("A")
	if !ok || ev == nil || ev.Kind != EventAction || ev.Action.ID != "task.accept" || ev.Row != "T1" || !ev.Stable {
		t.Fatalf("A on T1: %v %+v", ok, ev)
	}
	if _, ev := v.Key("enter"); ev == nil || ev.Action == nil || ev.Action.ID != "task.show" {
		t.Errorf("enter on T1: %+v", ev)
	}
	// D: T1 binds no delegate, and D is the core's: not the mod's "view".
	if ok, ev := v.Key("D"); ok || ev != nil {
		t.Errorf("D on T1: %v %+v", ok, ev)
	}
	if got := strings.Join(v.Render(40, 10, plainLook{}), "\n"); strings.Contains(got, "view") || !strings.Contains(got, "o open") {
		t.Errorf("the mod's D hint is drawn, or its o hint isn't:\n%s", got)
	}
	if b := v.Bound(); len(b) != 2 || b[0].Key != "A" || b[1].Key != "enter" {
		t.Errorf("Bound on T1: %+v", b)
	}
	v.Key("down")
	if _, ev := v.Key("D"); ev == nil || ev.Action == nil || ev.Action.ID != "task.delegate" {
		t.Errorf("D on T2: %+v", ev)
	}
	if _, ev := v.Key("A"); ev != nil {
		t.Errorf("A on T2, which binds no accept: %+v", ev)
	}
	if ev := v.Press("task.delegate"); ev == nil || ev.Kind != EventAction {
		t.Errorf("Press task.delegate: %+v", ev)
	}
	if ev := v.Press("task.accept"); ev != nil {
		t.Errorf("Press of an action T2 doesn't bind: %+v", ev)
	}
	// A remap: the console's keymap moves accept to Y.
	v.SetBindKeys(func(id string) []string {
		if id == "task.accept" {
			return []string{"Y"}
		}
		return defaultKeys(id)
	})
	v.Key("up")
	if _, ev := v.Key("Y"); ev == nil || ev.Action == nil || ev.Action.ID != "task.accept" {
		t.Errorf("Y after the remap: %+v", ev)
	}
}

// TestStable: a press counts only once the row's bindings, and the row
// under the cursor, had been unchanged for 300 ms; the view's first tree
// counts at once.
func TestStable(t *testing.T) {
	v, now := clockView(t)
	v.Set(mustDecode(t, bindTree))
	if _, ev := v.Key("A"); ev == nil || !ev.Stable {
		t.Fatalf("A at once on the first tree: %+v", ev)
	}
	// T1 says something else: it waits.
	v.Set(mustDecode(t, strings.Replace(bindTree, `"T1 one"`, `"T1 uno"`, 1)))
	if _, ev := v.Key("A"); ev == nil || ev.Stable {
		t.Fatalf("A at once after T1 changed: %+v", ev)
	}
	*now = now.Add(299 * time.Millisecond)
	if _, ev := v.Key("A"); ev.Stable {
		t.Error("A at 299 ms counted")
	}
	*now = now.Add(time.Millisecond)
	if _, ev := v.Key("A"); !ev.Stable {
		t.Error("A at 300 ms didn't count")
	}

	// The same tree again (T1's first text is a change back): stable
	// once it was still.
	v.Set(mustDecode(t, bindTree))
	*now = now.Add(modaction.Stable)
	v.Set(mustDecode(t, bindTree))
	if _, ev := v.Key("A"); !ev.Stable {
		t.Error("an unchanged republish reset the clock")
	}
	// T1's accept now targets T9: a switch, so not stable.
	v.Set(mustDecode(t, strings.Replace(bindTree, `"project":"p","task":"T1"}},{"id":"task.show"`, `"project":"p","task":"T9"}},{"id":"task.show"`, 1)))
	if _, ev := v.Key("A"); ev.Stable || ev.Action.Target["task"] != "T9" {
		t.Errorf("A after its target changed: %+v", ev)
	}
	*now = now.Add(time.Second)

	// The mod drops T1: the cursor lands on T2 without the human moving
	// it, so D there doesn't count yet, though T2's bindings are old.
	v.Set(mustDecode(t, strings.Replace(bindTree, `{"id":"T1","spans":[{"text":"T1 one"}],"actions":[{"id":"task.accept","target":{"project":"p","task":"T1"}},{"id":"task.show","target":{"project":"p","task":"T1"}}]},`, "", 1)))
	if v.Cursor("tasks") != "T2" {
		t.Fatalf("cursor %q", v.Cursor("tasks"))
	}
	if _, ev := v.Key("D"); ev == nil || ev.Stable {
		t.Errorf("D right after the cursor was moved under it: %+v", ev)
	}
	*now = now.Add(modaction.Stable)
	if _, ev := v.Key("D"); !ev.Stable {
		t.Error("D 300 ms later didn't count")
	}
	// T2 keeps its binding but says something else: it waits again.
	tree := strings.Replace(bindTree, `{"id":"T1","spans":[{"text":"T1 one"}],"actions":[{"id":"task.accept","target":{"project":"p","task":"T1"}},{"id":"task.show","target":{"project":"p","task":"T1"}}]},`, "", 1)
	v.Set(mustDecode(t, strings.Replace(tree, `"T2 two"`, `"T2 harmless"`, 1)))
	if _, ev := v.Key("D"); ev.Stable {
		t.Error("D right after T2's text changed counted")
	}
	*now = now.Add(time.Second)
	// T2 moves down a place, the cursor with it: it waits again.
	v.Set(mustDecode(t, strings.Replace(strings.Replace(tree, `"T2 two"`, `"T2 harmless"`, 1), `"tasks","rows":[`, `"tasks","rows":[{"id":"T0","spans":[{"text":"T0"}]},`, 1)))
	if v.Cursor("tasks") != "T2" {
		t.Fatalf("cursor %q", v.Cursor("tasks"))
	}
	if _, ev := v.Key("D"); ev.Stable {
		t.Error("D right after T2 moved under the cursor counted")
	}
	*now = now.Add(time.Second)
	// The human moving the cursor is no switch.
	v.Key("down")
	v.Key("up")
	if _, ev := v.Key("D"); !ev.Stable {
		t.Error("the human's own move made D wait")
	}
}

// TestViewActions: a keys widget's actions are bound whatever row the
// cursor is on, after the row's own, and wait on their own changes.
func TestViewActions(t *testing.T) {
	tree := strings.Replace(bindTree, `"keys":[{"key":"D","label":"view"},{"key":"o","label":"open"}]`,
		`"keys":[{"key":"o","label":"open"}],"actions":[{"id":"task.new","target":{"project":"p"}}]`, 1)
	v, now := clockView(t)
	v.Set(mustDecode(t, tree))
	if _, ev := v.Key("+"); ev == nil || ev.Action == nil || ev.Action.ID != "task.new" || !ev.Stable {
		t.Fatalf("+ on T1: %+v", ev)
	}
	v.Key("down")
	v.Key("down") // T3 binds nothing
	if b := v.Bound(); len(b) != 1 || b[0].Key != "+" || !b[0].View {
		t.Errorf("Bound on T3: %+v", b)
	}
	if ev := v.Press("task.new"); ev == nil || !ev.Stable {
		t.Errorf("Press task.new: %+v", ev)
	}
	// Its target changes: it waits; T3 moving doesn't make it.
	v.Set(mustDecode(t, strings.Replace(tree, `"task.new","target":{"project":"p"}`, `"task.new","target":{"project":"q"}`, 1)))
	if _, ev := v.Key("+"); ev == nil || ev.Stable {
		t.Errorf("+ right after its target changed: %+v", ev)
	}
	*now = now.Add(modaction.Stable)
	if _, ev := v.Key("+"); !ev.Stable {
		t.Errorf("+ 300 ms later: %+v", ev)
	}
	for _, js := range []string{
		`{"kind":"text","text":"x","actions":[{"id":"task.new","target":{"project":"p"}}]}`,
		`{"kind":"keys","actions":[{"id":"settings.open"}]}`,
	} {
		if _, err := Decode([]byte(js)); err == nil {
			t.Errorf("%s: no error", js)
		}
	}
}

// TestSelect: the console puts the cursor on the row bound to a task
// picked elsewhere, dropping a filter that hides it.
func TestSelect(t *testing.T) {
	v, _ := clockView(t)
	v.Set(mustDecode(t, bindTree))
	if !v.Select(modaction.Target{Project: "p", Task: "T2"}) || v.Cursor("tasks") != "T2" {
		t.Fatalf("Select T2: cursor %q", v.Cursor("tasks"))
	}
	if v.Select(modaction.Target{Project: "p", Task: "T3"}) || v.Select(modaction.Target{Project: "q", Task: "T1"}) {
		t.Error("Select of a task no row binds")
	}
	v.Key("/")
	for _, k := range []string{"o", "n", "e"} {
		v.Key(k)
	}
	if v.Cursor("tasks") != "T1" {
		t.Fatalf("filter one: cursor %q", v.Cursor("tasks"))
	}
	if !v.Select(modaction.Target{Project: "p", Task: "T2"}) || v.Cursor("tasks") != "T2" || v.Filter("tasks") != "" {
		t.Errorf("Select under a filter: cursor %q filter %q", v.Cursor("tasks"), v.Filter("tasks"))
	}
}

// TestUnbound: a core key the row doesn't bind names the action on the
// row's task, for the console to say why not; never on a bound key or a
// row that names no task.
func TestUnbound(t *testing.T) {
	v, _ := clockView(t)
	v.Set(mustDecode(t, bindTree))
	if a := v.Unbound("D"); a == nil || a.ID != "task.delegate" || a.Target["task"] != "T1" || a.Target["project"] != "p" {
		t.Errorf("D on T1: %+v", a)
	}
	if a := v.Unbound("A"); a != nil {
		t.Errorf("A on T1, which binds it: %+v", a)
	}
	if a := v.Unbound("o"); a != nil {
		t.Errorf("o, the mod's key: %+v", a)
	}
	v.Key("down")
	v.Key("down") // T3 names no task
	if a := v.Unbound("D"); a != nil {
		t.Errorf("D on T3: %+v", a)
	}
}
