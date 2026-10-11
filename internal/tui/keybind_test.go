package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/modaction"
)

// keymap applies km for the test and puts the defaults back after it.
func keymap(t *testing.T, km map[string][]string) error {
	t.Helper()
	t.Cleanup(func() { _ = ApplyKeymap(nil, "") })
	return ApplyKeymap(km, DefaultPrefixKey)
}

// wantIDs are the action ids, in table order. They are API (a keymap
// and the action binding name them): renaming one breaks every keymap
// that binds it, so a rename keeps the old id as an alias.
var wantIDs = []string{
	"list.up", "list.down", "list.page-up", "list.page-down", "focus.next", "focus.prev", "place.last", "row.open",
	"session.adopt", "project.activate", "card.fold", "project.new", "project.popup", "project.tasks", "project.inbox",
	"project.next", "project.prev", "list.narrower", "list.wider", "sidebar.narrower", "sidebar.wider",
	"sidebar.slim", "details.toggle", "settings.open", "help.open", "console.quit",
	"side.up", "side.down", "side.enter", "side.unfold", "side.fold", "side.back", "side.activate",
	"side.fold-all", "side.unfold-all", "side.pin", "side.first", "side.last", "side.page-up", "side.page-down",
	"side.nth", "side.needs-next", "side.needs-prev", "side.filter", "side.help",
	"popup.close", "repo.add", "repo.remove", "task.new", "task.show", "task.delegate", "task.accept", "task.send-back",
	"task.coordinator", "tasks.done", "library.read", "library.delete", "library.delete-thread",
	"session.attach", "session.dashboard", "session.remote", "prefix.send",
}

// TestActionIDs: every action has one stable id, unique and well formed,
// and the list is the one above. The escape hatches are fixed.
func TestActionIDs(t *testing.T) {
	var ids []string
	for _, a := range KeyActions() {
		if !actionID.MatchString(a.ID) {
			t.Errorf("id %q isn't dotted lower-case words", a.ID)
		}
		if slices.Contains(ids, a.ID) {
			t.Errorf("id %q twice", a.ID)
		}
		ids = append(ids, a.ID)
		if len(a.Keys) == 0 {
			t.Errorf("%s has no key", a.ID)
		}
	}
	if !slices.Equal(ids, wantIDs) {
		t.Errorf("ids changed:\n got %q\nwant %q", ids, wantIDs)
	}
	for _, a := range KeyActions() {
		escape := slices.ContainsFunc(a.Keys, func(k string) bool {
			return k == "esc" || k == "prefix+d" || k == "prefix+q" || k == "prefix+prefix"
		})
		if escape && !a.Fixed {
			t.Errorf("%s (%q) is an escape hatch, but can be rebound", a.ID, a.Keys)
		}
	}
}

// TestKeymapRefuses: a keymap can't take an escape hatch, rebind a fixed
// action, name an unknown one, put two actions on a key, or give a
// prefix command a key that means something else in a popup or the
// sidebar. What it can't apply is named; the rest applies.
func TestKeymapRefuses(t *testing.T) {
	for _, c := range []struct {
		name string
		km   map[string][]string
		want string
	}{
		{"esc", map[string][]string{"session.adopt": {"esc"}}, "session.adopt: esc is locked"},
		{"prefix+d", map[string][]string{"session.adopt": {"d"}}, "session.adopt: d is locked"},
		{"prefix+q", map[string][]string{"session.adopt": {"q"}}, "session.adopt: q is locked"},
		{"the prefix", map[string][]string{"session.adopt": {DefaultPrefixKey}}, "is locked"},
		{"a prefix form", map[string][]string{"session.adopt": {"prefix+x"}}, "is locked"},
		{"ctrl+c", map[string][]string{"side.pin": {"ctrl+c"}}, "side.pin: ctrl+c is locked"},
		{"quit", map[string][]string{"console.quit": {"Q"}}, "console.quit: can't be rebound"},
		{"tab", map[string][]string{"focus.next": {"o"}}, "focus.next: can't be rebound"},
		{"sidebar esc", map[string][]string{"side.back": {"h"}}, "side.back: can't be rebound"},
		{"popup", map[string][]string{"task.delegate": {"G"}}, "task.delegate: no such action"},
		{"unknown", map[string][]string{"nope.nothing": {"G"}}, "nope.nothing: no such action"},
		{"no key", map[string][]string{"session.adopt": {}}, "session.adopt: needs a key"},
		{"clash", map[string][]string{"session.adopt": {"n"}}, "session.adopt: its key is taken by project.new"},
		{"clash in the sidebar", map[string][]string{"side.pin": {"z"}}, "side.pin: its key is taken by side.fold-all"},
		{"a fixed key", map[string][]string{"side.pin": {"5"}}, "side.pin: its key is taken by side.nth"},
		{"popup key", map[string][]string{"project.next": {"x"}}, "project.next: a prefix command's key"},
		{"sidebar key", map[string][]string{"project.next": {"Z"}}, "project.next: a prefix command's key"},
		{"sidebar takes a prefix command", map[string][]string{"side.pin": {"]"}}, "side.pin: a prefix command's key"},
	} {
		t.Run(c.name, func(t *testing.T) {
			km := map[string][]string{}
			for k, v := range c.km {
				km[k] = v
			}
			km["sidebar.slim"] = []string{"B"} // a good one beside it applies
			err := keymap(t, km)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
			for id := range c.km {
				if binds.list.rebound(id) || binds.side.rebound(id) {
					t.Errorf("%s was rebound anyway", id)
				}
			}
			if !slices.Equal(binds.list.keys("sidebar.slim", nil), []string{"B"}) {
				t.Errorf("the good binding didn't apply")
			}
		})
	}
	// Swapping two actions' keys is fine.
	if err := keymap(t, map[string][]string{"project.next": {"["}, "project.prev": {"]"}}); err != nil {
		t.Fatal(err)
	}
	if binds.list.canon("[") != "]" || binds.list.canon("]") != "[" {
		t.Errorf("the swap runs [ as %q, ] as %q", binds.list.canon("["), binds.list.canon("]"))
	}
}

// TestKeymapRemaps: a rebound action runs from its new key on the list,
// after the prefix on the dashboard and in a session, and in the
// sidebar; its old key does nothing; the help, the footers, the menus
// and the hints show the new key.
func TestKeymapRemaps(t *testing.T) {
	if err := keymap(t, map[string][]string{
		"details.toggle": {"Y"}, "session.adopt": {"o"}, "side.pin": {"P"}, "side.fold": {"left"}, "help.open": {"?", "H"},
	}); err != nil {
		t.Fatal(err)
	}
	m := needsYou(t)
	before := m.layout.Details
	keyPress(m, "|")
	if m.layout.Details != before {
		t.Fatal("| still toggles the details panel")
	}
	keyPress(m, "Y")
	if m.layout.Details == before {
		t.Fatal("Y doesn't toggle the details panel")
	}
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	keyPress(m, "Y")
	if m.layout.Details != before {
		t.Fatal("prefix+Y doesn't toggle the details panel")
	}
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	keyPress(m, "H")
	if _, ok := m.top().(*helpView); !ok {
		t.Fatalf("prefix+H didn't open the help: %T", m.top())
	}
	help := ansi.Strip(strings.Join(m.top().(*helpView).box(m).body, "\n"))
	for _, want := range []string{"Y ", "? H ", "prefix+a i t , ? n", "prefix+Y ", "P ", "→ l ← "} {
		if !strings.Contains(help, "\n"+want) {
			t.Errorf("the help lacks a line starting %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "\n| ") || strings.Contains(help, "\np ") {
		t.Errorf("the help still lists | or p:\n%s", help)
	}
	keyPress(m, "esc")

	// The menus press the new keys, and say them.
	items := m.dashItems()
	if i := slices.IndexFunc(items, func(it menuItem) bool { return it.label == "details panel on / off" }); i < 0 || items[i].key != "Y" {
		t.Errorf("the ≡ menu's details item: %+v", items)
	}
	if !strings.Contains(prefixHint(), " Y details") || !strings.Contains(pendingList(), " Y info") {
		t.Errorf("the prefix hints don't say Y: %q, %q", prefixHint(), pendingList())
	}
	if c := (&client{dashboard: true}); !slices.ContainsFunc(c.sessionItems(), func(it aitem) bool { return it.key == "prefix+Y" }) {
		t.Errorf("the session's ≡ menu lacks prefix+Y")
	}

	// In a session, prefix+Y is the info panel's pane command, | nothing.
	prefix, _ := parseChord(DefaultPrefixKey)
	if do := prefixStep(prefix, true, uv.Key{Code: 'Y', Text: "Y"}, true); do.pane != "|" {
		t.Errorf("prefix+Y in a session: %+v", do)
	}
	if do := prefixStep(prefix, true, uv.Key{Code: '|', Text: "|"}, true); do != (prefixDo{}) {
		t.Errorf("prefix+| in a session still does %+v", do)
	}

	// The sidebar: P pins, p does nothing; its key bar says P.
	if op, ok := sideOp(binds.side.canon("P")); !ok || op != sidePin {
		t.Errorf("P in the sidebar is op %d", op)
	}
	if _, ok := sideOp(binds.side.canon("p")); ok {
		t.Errorf("p still works in the sidebar")
	}
	if bar := ansi.Strip(sideKeyBar(&treeRow{kind: treeProject}, true, false, 200)); !strings.Contains(bar, "P pin") {
		t.Errorf("the key bar doesn't say P: %q", bar)
	}
}

// TestBindableActions: every action a mod may bind (modaction) is one
// of the console's, under the same id and default key, so the key a
// row's hint shows is the key that runs it.
func TestBindableActions(t *testing.T) {
	for _, b := range modaction.Actions {
		i := slices.IndexFunc(KeyActions(), func(a KeyAction) bool { return a.ID == b.ID })
		if i < 0 {
			t.Errorf("%s is bindable but no console action", b.ID)
			continue
		}
		if a := KeyActions()[i]; !slices.Contains(a.Keys, b.Key) {
			t.Errorf("%s: bindable with key %q, the console's keys are %q", b.ID, b.Key, a.Keys)
		}
	}
}
