package tui

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Stable action ids and the remap layer (the mods design, §2 tier 2 and
// §11). Every action of the list (actions.go) and of the sidebar
// (sideActions) has an id, such as project.next, that names one
// behaviour whatever its keys; a keymap binds ids to keys
// (id = ["key", …]). The ids are API: a keymap mod and the action binding
// name them, so one is never renamed without keeping the old name as an
// alias.
//
// The tables keep their default keys, and the code behind them, the
// prefix commands (prefixCommands, paneCommands, prefixStep) and the
// menus speak those. The layer sits where a typed key comes in: canon
// turns it into the default key of the action it is bound to, so every
// path behind it is unchanged. The help, the footers and the menus show
// the bound keys (keyLabel, footLabel, boundKey).
//
// The escape hatches can't be taken: esc, prefix+d, prefix+q,
// prefix+prefix and ctrl+c, and the fixed actions (the area keys, the
// sidebar's esc and 1-9).

// actionID is the form of an id: dotted lower-case words.
var actionID = regexp.MustCompile(`^[a-z]+(\.[a-z]+(-[a-z]+)*)+$`)

// lockedKeys are never bound to an action: esc closes and goes back,
// ctrl+c quits, and d, q and r are prefix commands only (prefix+d,
// prefix+q, prefix+r), so a plain one would mean something else.
var lockedKeys = []string{"esc", "ctrl+c", "d", "q", "r"}

// popupOwnKeys are plain keys the popups, the settings and the menu use
// for their own (popupKeys, settingsKeys). A prefix command can't take
// one: over a popup, a plain key that is a prefix command must do
// nothing (TestNoPlainKeyIsAPrefixCommand).
var popupOwnKeys = []string{"1", "2", "3", "4", "5", "6", "7", "enter", "space", "+", "-", "x", "D", "A", "c",
	"up", "down", "left", "right", "pgup", "pgdown", "home", "end", "y"}

// bindable is one row of a key table, as the layer sees it.
type bindable struct {
	id          string
	keys        []string // the defaults
	fixed       bool
	label, help string
}

// listBindables and sideBindables are the two tables.
func listBindables() []bindable {
	out := make([]bindable, len(actions))
	for i, a := range actions {
		out[i] = bindable{a.id, a.keys, a.fixed, a.label, a.help}
	}
	return out
}

func sideBindables() []bindable {
	out := make([]bindable, len(sideActions))
	for i, a := range sideActions {
		out[i] = bindable{a.id, a.keys, a.fixed, a.label, a.help}
	}
	return out
}

// keyTable is one table's keymap: the ids it rebinds, and what a typed
// key runs as.
type keyTable struct {
	// bound are the keys of each rebound id.
	bound map[string][]string
	// canonOf turns a typed key into the default key it runs as: a bound
	// key into its action's first default, a default key no action has
	// any more into "".
	canonOf map[string]string
}

// binds are this console's keymaps, the defaults until ApplyKeymap.
var binds struct{ list, side keyTable }

// canon is the default key typed key runs as.
func (t keyTable) canon(key string) string {
	if c, ok := t.canonOf[key]; ok {
		return c
	}
	return key
}

// keys are the keys id is bound to; def are its defaults.
func (t keyTable) keys(id string, def []string) []string {
	if k, ok := t.bound[id]; ok {
		return k
	}
	return def
}

// rebound says whether a keymap moved id off its defaults.
func (t keyTable) rebound(id string) bool {
	_, ok := t.bound[id]
	return ok
}

// tableOf is the table an id is in, nil for none.
func tableOf(id string) *keyTable {
	switch {
	case strings.HasPrefix(id, "side."):
		return &binds.side
	case id != "":
		return &binds.list
	}
	return nil
}

// boundKey is the first key of the list's action whose first default key
// is def, def when none rebinds it: what a menu or a hint presses and
// shows.
func boundKey(def string) string { return binds.list.boundOf(listBindables(), def) }

// boundKeys are defs' bound keys, as a hint lists them.
func boundKeys(defs ...string) string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = keyShown(boundKey(d))
	}
	return strings.Join(out, " ")
}

func (t keyTable) boundOf(rows []bindable, def string) string {
	for _, r := range rows {
		if len(r.keys) > 0 && r.keys[0] == def {
			if k := t.keys(r.id, r.keys); len(k) > 0 {
				return k[0]
			}
		}
	}
	return def
}

// keyShown is how the help writes a key: the arrows as arrows.
func keyShown(k string) string {
	switch k {
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	}
	return k
}

// keyLabel is the keys column of rows[i]'s help line: its label, or with
// a rebound action in its line (it and the rows after it with neither
// label nor help), their bound keys.
func keyLabel(t keyTable, rows []bindable, i int) string {
	line := []bindable{rows[i]}
	for _, r := range rows[i+1:] {
		if r.label != "" || r.help != "" {
			break
		}
		line = append(line, r)
	}
	if !slices.ContainsFunc(line, func(r bindable) bool { return t.rebound(r.id) }) {
		return rows[i].label
	}
	var keys []string
	for _, r := range line {
		for _, k := range t.keys(r.id, r.keys) {
			keys = append(keys, keyShown(k))
		}
	}
	return strings.Join(keys, " ")
}

// footLabel is a's key in the footer: its label, or its first bound key
// when rebound.
func footLabel(a action) string {
	if !binds.list.rebound(a.id) {
		return a.label
	}
	return keyShown(binds.list.keys(a.id, a.keys)[0])
}

// barKey is a hint's key for id: shown (its default, as the hint writes
// it) unless a keymap rebinds id, then its first bound key.
func barKey(id, shown string) string {
	t := tableOf(id)
	if t == nil || !t.rebound(id) {
		return shown
	}
	return keyShown(t.bound[id][0])
}

// ApplyKeymap sets this console's keymap: each action id to its keys
// (id = ["key", …]), replacing its defaults; prefix is the prefix key,
// which no action can take. An entry that can't be applied is left out,
// its action keeping its defaults, and named in the error; the rest
// apply. A nil or empty keymap is the defaults. It is set before the
// console starts: the keys are read without a lock.
func ApplyKeymap(km map[string][]string, prefix string) error {
	var errs []error
	list, side := listBindables(), sideBindables()
	tables := []struct {
		t    *keyTable
		rows []bindable
		want map[string][]string
	}{{&binds.list, list, map[string][]string{}}, {&binds.side, side, map[string][]string{}}}
	for _, id := range slices.Sorted(maps.Keys(km)) {
		ti := 0
		if tableOf(id) == &binds.side {
			ti = 1
		}
		var row *bindable
		if i := slices.IndexFunc(tables[ti].rows, func(r bindable) bool { return r.id == id }); i >= 0 {
			row = &tables[ti].rows[i]
		}
		if err := checkBinding(row, id, km[id], prefix); err != nil {
			errs = append(errs, err)
			continue
		}
		tables[ti].want[id] = slices.Clone(km[id])
	}
	// Drop what clashes, until nothing does: two actions on a key, or a
	// prefix command on a key that means something else elsewhere.
	drop := func(want map[string][]string, id, why string) bool {
		if _, ok := want[id]; !ok {
			return false
		}
		delete(want, id)
		errs = append(errs, fmt.Errorf("%s: %s", id, why))
		return true
	}
	for dropped := true; dropped; {
		dropped = false
		for _, tb := range tables {
			for _, ids := range clashes(tb.rows, tb.want) {
				for _, id := range ids {
					others := slices.DeleteFunc(slices.Clone(ids), func(o string) bool { return o == id })
					dropped = drop(tb.want, id, "its key is taken by "+strings.Join(others, ", ")) || dropped
				}
			}
		}
		for _, id := range prefixClashes(list, tables[0].want, side, tables[1].want) {
			for _, tb := range tables {
				dropped = drop(tb.want, id, "a prefix command's key can't mean something else in a popup or the sidebar") || dropped
			}
		}
	}
	for _, tb := range tables {
		*tb.t = newKeyTable(tb.rows, tb.want)
	}
	return errors.Join(errs...)
}

// checkBinding is what is wrong with binding id (row, nil when no table
// has it) to keys on their own.
func checkBinding(row *bindable, id string, keys []string, prefix string) error {
	switch {
	case !actionID.MatchString(id) || row == nil:
		return fmt.Errorf("%s: no such action", id)
	case row.fixed:
		return fmt.Errorf("%s: can't be rebound", id)
	case len(keys) == 0:
		return fmt.Errorf("%s: needs a key", id)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		switch {
		case k == "" || strings.ContainsAny(k, " \t\n"):
			return fmt.Errorf("%s: %q isn't a key", id, k)
		case slices.Contains(lockedKeys, k), k == prefix, strings.HasPrefix(k, "prefix+"):
			return fmt.Errorf("%s: %s is locked (esc, ctrl+c, the prefix and the prefix commands d q r can't be taken)", id, k)
		case seen[k]:
			return fmt.Errorf("%s: lists %s twice", id, k)
		}
		seen[k] = true
	}
	return nil
}

// clashes are the groups of ids that share a key with wants applied.
func clashes(rows []bindable, wants map[string][]string) [][]string {
	on := map[string][]string{}
	for _, r := range rows {
		k := r.keys
		if w, ok := wants[r.id]; ok {
			k = w
		}
		for _, key := range k {
			on[key] = append(on[key], r.id)
		}
	}
	var out [][]string
	for _, key := range slices.Sorted(maps.Keys(on)) {
		if len(on[key]) > 1 {
			out = append(out, on[key])
		}
	}
	return out
}

// prefixClashes are the rebound ids that break the key table's rule (SPEC
// §4): prefix then a key means what the key means, so a prefix command's
// key (a list action reached after the prefix) does nothing as a plain
// key in a popup and the same in the sidebar (only ? is in both, the help).
func prefixClashes(list []bindable, lw map[string][]string, side []bindable, sw map[string][]string) []string {
	keysOf := func(r bindable, w map[string][]string) []string {
		if k, ok := w[r.id]; ok {
			return k
		}
		return r.keys
	}
	sideOn := map[string]string{}
	for _, r := range side {
		for _, k := range keysOf(r, sw) {
			sideOn[k] = r.id
		}
	}
	var out []string
	for _, r := range list {
		if len(r.keys) == 0 || !prefixCommands[r.keys[0]] && !paneCommands[r.keys[0]] {
			continue
		}
		for _, k := range keysOf(r, lw) {
			s, inSide := sideOn[k]
			_, lRe := lw[r.id]
			_, sRe := sw[s]
			switch {
			case lRe && slices.Contains(popupOwnKeys, k):
				out = append(out, r.id)
			case inSide && (r.id != "help.open" || s != "side.help") && (lRe || sRe):
				if lRe {
					out = append(out, r.id)
				}
				if sRe {
					out = append(out, s)
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// newKeyTable is rows' keymap with bound applied: each bound key runs
// as its action's first default, and a rebound action's default keys
// that no action took again run nothing.
func newKeyTable(rows []bindable, bound map[string][]string) keyTable {
	t := keyTable{bound: bound, canonOf: map[string]string{}}
	for _, r := range rows {
		if _, ok := bound[r.id]; ok {
			for _, k := range r.keys {
				t.canonOf[k] = ""
			}
		}
	}
	for _, r := range rows {
		for _, k := range bound[r.id] {
			t.canonOf[k] = r.keys[0]
		}
	}
	return t
}

// coreActions are the actions of the popups and the prefix, which the
// action binding names (a mod's row binds task.delegate, never a key);
// they are fixed for now. Their keys and help are the help's (popupKeys,
// sessionKeys).
var coreActions = []struct {
	area string
	bindable
}{
	{"popup", bindable{id: "popup.close", keys: []string{"esc"}, fixed: true, help: "close the popup"}},
	{"popup", bindable{id: "repo.add", keys: []string{"+"}, fixed: true, help: "add a repository (the overview)"}},
	{"popup", bindable{id: "repo.remove", keys: []string{"x"}, fixed: true, help: "remove the selected repository (the overview; asks first)"}},
	{"popup", bindable{id: "task.new", keys: []string{"+"}, fixed: true, help: "a new task (the tasks), from a task template or none"}},
	{"popup", bindable{id: "task.show", keys: []string{"enter"}, fixed: true, help: "show the task"}},
	{"popup", bindable{id: "task.delegate", keys: []string{"D"}, fixed: true, help: "delegate the task; the coordinator starts a thread for it (asks first)"}},
	{"popup", bindable{id: "task.accept", keys: []string{"A"}, fixed: true, help: "accept the task in review; the coordinator marks it done (asks first)"}},
	{"popup", bindable{id: "task.send-back", keys: []string{"x"}, fixed: true, help: "send the task in review back with a note"}},
	{"popup", bindable{id: "task.coordinator", keys: []string{"c"}, fixed: true, help: "open the project's coordinator about the task"}},
	{"popup", bindable{id: "tasks.done", keys: []string{"b"}, fixed: true, help: "list the done tasks, or collapse them"}},
	{"popup", bindable{id: "library.read", keys: []string{"enter"}, fixed: true, help: "read the library file"}},
	{"popup", bindable{id: "library.delete", keys: []string{"d"}, fixed: true, help: "delete the library file (asks first)"}},
	{"popup", bindable{id: "library.delete-thread", keys: []string{"D"}, fixed: true, help: "delete all of the thread's library files (asks first)"}},
	{"session", bindable{id: "session.dashboard", keys: []string{"prefix+d"}, fixed: true, help: "back to the dashboard"}},
	{"session", bindable{id: "session.remote", keys: []string{"prefix+r"}, fixed: true, help: "remote control of a coordinator on or off (asks first)"}},
	{"session", bindable{id: "prefix.send", keys: []string{"prefix+prefix"}, fixed: true, help: "send the prefix key to the program"}},
}

// KeyAction is one action as the action binding and a keymap see it.
type KeyAction struct {
	// ID is its stable name; Area where it works ("list", "sidebar",
	// "popup" or "session"); Keys its keys now; Help its line in the help ("" for
	// one that shares the line above); Fixed that it can't be rebound.
	ID, Area string
	Keys     []string
	Help     string
	Fixed    bool
}

// KeyActions are every action, the list's, the sidebar's, then the
// popups' and the prefix's, with the keys this console's keymap gives
// them.
func KeyActions() []KeyAction {
	var out []KeyAction
	for _, tb := range []struct {
		area string
		t    keyTable
		rows []bindable
	}{{"list", binds.list, listBindables()}, {"sidebar", binds.side, sideBindables()}} {
		for _, r := range tb.rows {
			out = append(out, KeyAction{ID: r.id, Area: tb.area, Keys: slices.Clone(tb.t.keys(r.id, r.keys)), Help: r.help, Fixed: r.fixed})
		}
	}
	for _, c := range coreActions {
		out = append(out, KeyAction{ID: c.id, Area: c.area, Keys: slices.Clone(c.keys), Help: c.help, Fixed: true})
	}
	return out
}
