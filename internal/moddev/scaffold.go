// Package moddev holds the mod author's tools (mods design §8, Dev
// workflow): tm mod new's scaffolds, and the folder watch and log tail
// of tm mod dev and tm mod logs. tm mod test is package modtest.
package moddev

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modapi"
)

// Kinds are what tm mod new scaffolds: two code mods (a sidebar column,
// a hook on events) and four data mods.
var Kinds = []string{"column", "hook", "theme", "icons", "keymap", "rules"}

// IsCode reports whether kind is a code mod's.
func IsCode(kind string) bool { return kind == "column" || kind == "hook" }

// Scaffold returns the files of a new mod called id of kind, by path
// relative to its folder. A code mod gets its main.ts, the mod API's
// types (types/tm.d.ts, from this tm), a tsconfig.json for editors and
// tsc, and a fixture for tm mod test; a data mod its data file.
func Scaffold(id, kind string) (map[string]string, error) {
	if err := mod.CheckID(id); err != nil {
		return nil, err
	}
	if !slices.Contains(Kinds, kind) {
		return nil, fmt.Errorf("no kind %q: one of %s", kind, strings.Join(Kinds, ", "))
	}
	name := Title(id)
	head := fmt.Sprintf("id = %q\nname = %q\nversion = \"0.1.0\"\napi = %q\ndescription = %q\nauthors = []\n",
		id, name, mod.APIVersion(), descriptions[kind])
	files := map[string]string{"README.md": readme(id, name, kind)}
	switch kind {
	case "column":
		files["mod.toml"] = head + `
[provides]
main = "main.ts"

# The column, shown in layouts as ` + id + `.state.
[[provides.columns]]
id = "state"
title = "state"
width = 5
where = ["sidebar.thread"]

[permissions]
project_read = true
`
		files["main.ts"] = columnTS
		files["tests/waiting.json"] = columnFixture
	case "hook":
		files["mod.toml"] = head + `
[provides]
main = "main.ts"

[permissions]
project_read = true

[settings.status]
type = "string"
default = "review"
label = "The task status to tell you about"
`
		files["main.ts"] = hookTS
		files["tests/review.json"] = hookFixture
	case "theme":
		files["mod.toml"] = head + "\n[provides]\ntheme = \"theme.toml\"\n"
		files["theme.toml"] = themeTOML
	case "icons":
		files["mod.toml"] = head + "\n[provides]\nicons = \"icons.toml\"\n"
		files["icons.toml"] = fmt.Sprintf(iconsTOML, name)
	case "keymap":
		files["mod.toml"] = head + "\n[provides]\nkeymap = \"keymap.toml\"\n"
		files["keymap.toml"] = keymapTOML
	case "rules":
		files["mod.toml"] = head + "\n[provides.rules]\ncoordinator = \"rules/coordinator.md\"\n"
		files["rules/coordinator.md"] = rulesMD
	}
	if IsCode(kind) {
		files["types/tm.d.ts"] = modapi.TS()
		files["tsconfig.json"] = tsconfig
	}
	return files, nil
}

// Write writes files into dir, which must not exist or be empty, and
// returns their paths, sorted.
func Write(dir string, files map[string]string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	switch {
	case err == nil && len(ents) > 0:
		return nil, fmt.Errorf("%s isn't empty", dir)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(files[n]), 0o644); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// Title is id as a name: "review-age" is "Review age".
func Title(id string) string {
	s := strings.ReplaceAll(id, "-", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var descriptions = map[string]string{
	"column": "A sidebar column with each thread's state.",
	"hook":   "Tells you when a task reaches review.",
	"theme":  "A theme: the dashboard's colours, light and dark.",
	"icons":  "An icon set.",
	"keymap": "Other keys for the dashboard's actions.",
	"rules":  "A rules addendum for the coordinator.",
}

func readme(id, name, kind string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", name, descriptions[kind])
	if IsCode(kind) {
		b.WriteString("A code mod: main.ts runs in a mod host, and reaches tm only through `$`\n" +
			"(types/tm.d.ts, written by the tm that made this folder).\n\n" +
			"    tm mod dev .      # load it from this folder, restart it on every save, tail its log\n" +
			"    tm mod test .     # replay tests/*.json against it with a fake broker\n" +
			"    tm mod logs " + id + " -f\n\n" +
			"A fixture in tests/ names the events to send, what the broker answers\n" +
			"and the calls the mod must make (docs/SPEC.md §5.3, tm mod test).\n")
	} else {
		b.WriteString("A data mod: tm reads its files; nothing runs.\n\n" +
			"    tm mod dev .      # load it from this folder\n" +
			"    tm mod validate .\n")
	}
	fmt.Fprintf(&b, "\nInstall it with `tm mod install <this folder>`, then `tm mod enable %s`.\n", id)
	return b.String()
}

const tsconfig = `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2017",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "lib": ["ES2017"],
    "types": []
  },
  "include": ["main.ts", "types/tm.d.ts"]
}
`

const columnTS = `// A sidebar column: each thread's state, red when it needs you.
import type { TM } from "./types/tm";

const short: Record<string, string> = { working: "work", waiting: "wait", idle: "idle", stopped: "stop", resolved: "done" };

$.on("thread.state", (e: TM.ThreadState) => {
  const text = short[e.state] ?? e.state.slice(0, 5);
  $.ui.cell.set("state", { project: e.project, thread: e.thread }, [{ text, role: e.needs_you ? "bad" : "faint" }]);
});

$.on("thread.resolved", (e) => {
  $.ui.cell.set("state", { project: e.project, thread: e.thread }, null);
});
`

const columnFixture = `{
  "name": "a thread that needs you shows red",
  "events": [
    { "name": "thread.state", "project": "demo",
      "data": { "project": "demo", "thread": "t-1", "state": "waiting", "needs_you": "a question" } }
  ],
  "expect": {
    "calls": [
      { "method": "ui.cell.set",
        "params": { "column": "state", "target": { "project": "demo", "thread": "t-1" }, "spans": [{ "text": "wait", "role": "bad" }] } }
    ]
  }
}
`

const hookTS = `// A hook: a toast when a task reaches the status in the mod's settings.
import type { TM } from "./types/tm";

$.on("task.changed", (e: TM.TaskChanged) => {
  const status = String($.settings().status);
  if (e.to !== status || e.from === status) return;
  $.log(` + "`${e.project} ${e.id} is now ${status}`" + `);
  $.ui.toast(` + "`${e.id} ${e.title} is ready for ${status}`" + `, "info");
});
`

const hookFixture = `{
  "name": "a task reaching review is told",
  "events": [
    { "name": "task.changed", "project": "demo",
      "data": { "project": "demo", "id": "T1", "title": "Docs", "from": "doing", "to": "review", "steps_done": 2, "steps_total": 2 } },
    { "name": "task.changed", "project": "demo",
      "data": { "project": "demo", "id": "T2", "title": "Fix", "from": "todo", "to": "doing", "steps_done": 0, "steps_total": 1 } }
  ],
  "expect": {
    "calls": [{ "method": "ui.toast", "params": { "text": "T1 Docs is ready for review", "level": "info" } }],
    "logs": ["demo T1 is now review"]
  }
}
`

const themeTOML = `# A theme: each role's look, for a light and a dark terminal. A colour is
# an ANSI name (red, brightblue…), a 0-255 index or #rrggbb; a second
# entry is the truecolor choice, the first its ANSI fallback. A role left
# out keeps the built-in look. Roles: head sel faint accent title bad
# warn good info plain.

[light.accent]
fg = ["blue", "#5e81ac"]

[light.title]
fg = ["blue", "#5e81ac"]
bold = true

[dark.accent]
fg = ["cyan", "#88c0d0"]

[dark.title]
fg = ["cyan", "#88c0d0"]
bold = true
`

const iconsTOML = `# An icon set: each glyph one cell wide (a tree connector two), no emoji.
# base is the built-in set (unicode, nerd or ascii) that fills every glyph
# left out.
name = %q
base = "unicode"

working = "▶"
idle = "·"
done = "✔"
`

const keymapTOML = `# Keys by action id (the ids in tm's help, ?): each action gets these
# keys in place of its defaults. Locked keys (tab, esc, the prefix)
# can't be taken.

"list.up" = ["up", "k", "ctrl+p"]
"list.down" = ["down", "j", "ctrl+n"]
`

const rulesMD = `## House rules

- Name a branch after its task: t<n>-<a few words>.
`
