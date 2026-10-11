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
// under the cursor, had been unchanged for 300 ms.
func TestStable(t *testing.T) {
	v, now := clockView(t)
	v.Set(mustDecode(t, bindTree))
	if _, ev := v.Key("A"); ev == nil || ev.Stable {
		t.Fatalf("A at once: %+v", ev)
	}
	*now = now.Add(299 * time.Millisecond)
	if _, ev := v.Key("A"); ev.Stable {
		t.Error("A at 299 ms counted")
	}
	*now = now.Add(time.Millisecond)
	if _, ev := v.Key("A"); !ev.Stable {
		t.Error("A at 300 ms didn't count")
	}

	// The same tree again: still stable.
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
