package mod

import (
	"slices"
	"strings"
	"testing"
)

// messyCode is a code mod with a problem in every provides list, its
// permissions, settings and defaults.
const messyCode = `id = "messy"
name = "M"
version = "1.0.0"
api = "1.6"
homepage = "ftp://x"

[provides]
main = "m.ts"
code_host = "github"
columns = [
  { id = "a", width = 2, where = ["sidebar.thread"] },
  { id = "a", width = 0, where = [] },
  { id = "B", width = 2, where = ["nowhere"] },
]
views = [
  { id = "v", title = "V", slot = "sidebar.section" },
  { id = "v", title = " ", slot = "pane", opens = "zz" },
  { id = "W_", title = "W", slot = "full" },
]

[[provides.guard]]
id = "merge"
commands = ["/bin/rm"]
writes = ["rel", "~/a/../b"]
roles = ["human"]
message = " "

[[provides.guard]]
id = "g"
message = "m"

[[provides.guard]]
id = "g"
message = "m"

[[provides.guard]]
id = "G"
commands = ["a"]
message = "m"

[[provides.tools]]
id = "t"
description = "T"
roles = ["human", "thread"]
input = { properties = { Bad = { type = "string" }, n = { type = "blob" } }, required = ["missing"] }

[[provides.tools]]
id = "t"

[[provides.tools]]
id = "Tee"
description = "T"

[[provides.commands]]
id = "c"
title = "C"
key = "g"

[[provides.commands]]
id = "c"
title = " "
key = "g"

[[provides.commands]]
id = "C!"
title = "C"
key = "esc"

[permissions]
net = ["https://x"]
commands = ["/bin/sh"]
actions = ["thread.merge", "task.accept"]
files = { read = ["notes", "/", "~/./"], write = ["~/x/../y"] }

[settings]
Bad = { type = "int", default = 0, min = 2, max = 1 }
t = { type = "string", default = "a\u001b[31m", label = "x\ny" }
o = { type = "choice", default = "a", options = ["a", "b\u0007"] }
c = { type = "choice", default = "a" }
s = { type = "string", min = 1 }

[defaults]
nope = 1
`

// messyData is a data mod that lists what only code provides.
const messyData = `id = "messy"
name = " "
version = "v1"
api = "1"
min_tm = "x"

[provides]
columns = [{ id = "a", width = 2, where = ["sidebar.thread"] }, { id = "a", width = 2, where = ["sidebar.thread"] }]
views = [{ id = "v", title = "V", slot = "full" }]
tools = [{ id = "t", description = "T" }]
commands = [{ id = "c", title = "C" }]
code_host = "glab"
rules = { human = "r.md" }

[permissions]
inbox = true
`

// TestManifestCheckSections pins every problem check reports for two
// manifests with something wrong in each section, as lines in any
// order.
func TestManifestCheckSections(t *testing.T) {
	for _, tc := range []struct {
		name, toml string
		want       []string
	}{
		{"code", messyCode, []string{
			"defaults.nope: a mod may suggest only " + strings.Join(DefaultKeys, ", "),
			`homepage "ftp://x" isn't a web address`,
			"permissions.actions needs permissions.project_read: a binding is checked against the projects it acts in",
			`permissions.actions: "thread.merge" is not an action a mod may bind`,
			"permissions.actions: task.accept needs no grant of its own; list only session.clear, question.answer",
			`permissions.commands: "/bin/sh" isn't a program name (no path, no arguments)`,
			`permissions.files: "notes" must be absolute or start with ~/`,
			`permissions.files: "~/x/../y" has a .. in it`,
			`permissions.files: "/" is the home folder, holds it or is a disk's root; name the folder the mod needs`,
			`permissions.files: "~/./" is the home folder, holds it or is a disk's root; name the folder the mod needs`,
			`permissions.net: "https://x" isn't a host name (no scheme, port or path)`,
			`provides.code_host: "github" must be lowercase letters, digits and dashes, and not github or azure (tm's own hosts)`,
			`provides.columns[1]: id "a" is listed twice`,
			"provides.columns[1]: where names no slot",
			"provides.columns[1]: width must be 1 to 12 cells",
			`provides.columns[2]: id "B" must be lowercase letters, digits and dashes`,
			`provides.columns[2]: where "nowhere" isn't a slot like sidebar.thread`,
			"provides.commands[1]: has no title",
			`provides.commands[1]: id "c" is listed twice`,
			"provides.commands[1]: key g is c's already",
			`provides.commands[2]: id "C!" must be lowercase letters, digits and dashes`,
			"provides.commands[2]: key esc is locked (esc, ctrl+c, d, q, r, tab, enter and 1-9 stay the core's)",
			`provides.guard[0]: "rel" must be absolute or start with ~/`,
			`provides.guard[0]: "~/a/../b" has a .. in it`,
			`provides.guard[0]: command "/bin/rm" must start with a program's name (no path, no *)`,
			"provides.guard[0]: has no message: say why, and what to do instead",
			`provides.guard[0]: id "merge" is a core rule's: a mod adds rules, it doesn't change the core's`,
			`provides.guard[0]: role "human" must be thread or coordinator`,
			"provides.guard[1]: refuses nothing: list commands, writes or reads",
			`provides.guard[2]: id "g" is listed twice`,
			"provides.guard[2]: refuses nothing: list commands, writes or reads",
			`provides.guard[3]: id "G" must be lowercase letters, digits and dashes`,
			"provides.tools needs permissions.tools",
			`provides.tools[0]: input property "Bad" must be lowercase letters, digits and underscores`,
			`provides.tools[0]: input property "n" needs a type: string, integer, number, boolean, array, object`,
			`provides.tools[0]: required "missing" isn't an input property`,
			`provides.tools[0]: role "human" must be thread or coordinator`,
			"provides.tools[1]: has no description",
			`provides.tools[1]: id "t" is listed twice`,
			`provides.tools[2]: id "Tee" must be lowercase letters, digits and underscores, starting with a letter`,
			"provides.views needs permissions.views",
			`provides.views[0]: slot sidebar.section needs api = "1.7"`,
			"provides.views[1]: has no title",
			`provides.views[1]: id "v" is listed twice`,
			"provides.views[1]: opens is a section's: only a sidebar.section, sidebar.project.section, coordinator.section or details.section opens a view",
			`provides.views[1]: slot "pane" must be one of ` + strings.Join(Slots, ", "),
			`provides.views[2]: id "W_" must be lowercase letters, digits and dashes`,
			"settings.Bad: a setting's name is lowercase letters, digits and underscores",
			"settings.Bad: default must be 2 to 1",
			"settings.Bad: min is above max",
			"settings.c: a choice needs options",
			"settings.c: default must be one of ",
			"settings.s: has no default",
			"settings.s: min and max are for an int",
			"settings.t: default must be one line, without control or format characters",
			"settings.t: label must be one line, without control or format characters",
			`settings.o: option "b\a" must be one line, without control or format characters`,
		}},
		{"data", messyData, []string{
			`api "1" isn't MAJOR.MINOR`,
			"has no name",
			`min_tm "x" isn't a tm version`,
			"permissions are for code mods (provides.main); a data mod needs none",
			"provides.code_host needs permissions.commands: the host's CLI it runs (glab)",
			"provides.code_host: a code host needs provides.main, the code that answers it",
			"provides.columns[0]: a column needs provides.main, the code that fills it",
			"provides.columns[1]: a column needs provides.main, the code that fills it",
			"provides.commands[0]: a command needs provides.main, the code that runs it",
			"provides.rules.human: rules are coordinator or thread (addenda), coordinator_policy or thread_policy",
			"provides.tools needs permissions.tools",
			"provides.tools[0]: a tool needs provides.main, the code that answers it",
			"provides.views needs permissions.views",
			"provides.views[0]: a view needs provides.main, the code that draws it",
			`version "v1" isn't MAJOR.MINOR.PATCH`,
		}},
	} {
		_, err := ParseManifest([]byte(tc.toml), "")
		if err == nil {
			t.Fatalf("%s: no error", tc.name)
		}
		got := strings.Split(err.Error(), "\n")
		slices.Sort(got)
		want := slices.Sorted(slices.Values(tc.want))
		if !slices.Equal(got, want) {
			t.Errorf("%s: got\n%s\nwant\n%s", tc.name, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// TestCheckIDs: an entry's needs comes before its id's form, its form
// before also, and also before a second listing.
func TestCheckIDs(t *testing.T) {
	core := func(id string) string {
		if id == "merge" {
			return "core"
		}
		return ""
	}
	code := &Manifest{Provides: Provides{Main: "m.ts"}}
	got := code.checkIDs("guard", "", []string{"a", "a", "B", "merge", "merge"}, dashIDs, core)
	want := []string{
		`provides.guard[1]: id "a" is listed twice`,
		`provides.guard[2]: id "B" must be lowercase letters, digits and dashes`,
		"provides.guard[3]: core",
		"provides.guard[4]: core",
	}
	if s := errStrings(got); !slices.Equal(s, want) {
		t.Errorf("code: %q, want %q", s, want)
	}
	data := &Manifest{}
	got = data.checkIDs("tools", "needs main", []string{"t", "T"}, underIDs, nil)
	if s := errStrings(got); !slices.Equal(s, []string{"provides.tools[0]: needs main", "provides.tools[1]: needs main"}) {
		t.Errorf("data: %q", s)
	}
	if got := data.checkIDs("guard", "", []string{"g"}, dashIDs, nil); len(got) != 0 {
		t.Errorf("a rule needs no code: %v", got)
	}
}

func TestCheckRoles(t *testing.T) {
	got := errStrings(checkRoles("x", []string{RoleThread, "human", RoleCoordinator, ""}))
	want := []string{`x: role "human" must be thread or coordinator`, `x: role "" must be thread or coordinator`}
	if !slices.Equal(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
}

func errStrings(errs []error) []string {
	var s []string
	for _, e := range errs {
		s = append(s, e.Error())
	}
	return s
}

func TestCheckLimits(t *testing.T) {
	n := func(k int) []string {
		out := make([]string, k)
		for i := range out {
			out[i] = "h" + strings.Repeat("a", i) + ".example.com"
		}
		return out
	}
	m := &Manifest{
		Name:        strings.Repeat("n", maxNameLen+1),
		Description: strings.Repeat("d", maxDescLen+1),
		Authors:     make([]string, maxAuthors+1),
		Provides:    Provides{Main: "m.ts", Columns: make([]Column, maxColumns+1), Views: make([]View, maxViews+1)},
		Permissions: Permissions{Net: n(maxNetHosts + 1), Commands: []string{"git", "git"}, Files: Files{Read: []string{"~/a", "~/a"}}},
		Settings:    map[string]Setting{"c": {Type: SettingChoice, Options: make([]string, maxOptions+1)}},
	}
	var got []error
	got = append(got, m.checkMeta()...)
	got = append(got, m.checkColumns()...)
	got = append(got, m.checkViews()...)
	got = append(got, m.checkPermissions()...)
	got = append(got, m.checkSettings()...)
	all := strings.Join(errStrings(got), "\n")
	for _, w := range []string{
		"name is longer than", "description is longer than", "authors: at most", "provides.columns: at most",
		"provides.views: at most", "permissions.net: at most", `permissions.commands: "git" is listed twice`,
		`permissions.files.read: "~/a" is listed twice`, "settings.c: at most 32 options",
	} {
		if !strings.Contains(all, w) {
			t.Errorf("no problem %q in:\n%s", w, all)
		}
	}
}
