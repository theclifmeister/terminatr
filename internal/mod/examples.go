package mod

// terminatr's examples/mods folder (mods design §13): one small, working
// mod per mod surface, so people see each surface working and get ideas
// to build and share. They are sample mods, not defaults: nothing in the
// folder is loaded or enabled, though four of them are the sources of
// built-in mods. The Mods tab's e, its empty state and tm mod examples
// show the link and each one's install lines, and do nothing else.

import (
	"regexp"

	"github.com/theclifmeister/terminatr/internal/version"
)

// ExamplesRepo is terminatr's repository.
const ExamplesRepo = "https://github.com/theclifmeister/terminatr"

// ExamplesPath is the folder in a checkout of ExamplesRepo.
const ExamplesPath = "examples/mods"

// ExamplesRef is the ref the install lines pin: this tm's release tag,
// so an example speaks the mod API this tm does (main's may already
// need the next release) and tm mod update moves it on to the newest
// tag; main for a build that isn't a release.
var ExamplesRef = examplesRef(version.Channel, version.Version)

func examplesRef(channel, v string) string {
	if channel == "release" && releaseTagRE.MatchString(v) {
		return v
	}
	return "main"
}

var releaseTagRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// ExamplesURL is the folder in terminatr's repository, at ExamplesRef.
var ExamplesURL = ExamplesRepo + "/tree/" + ExamplesRef + "/" + ExamplesPath

// Example is one mod of the folder: its id (and folder name), the
// surface it shows and the permissions its mod.toml asks for.
type Example struct {
	ID, Surface string
	Asks        Permissions
}

// Perms are the permissions the example asks for, by name.
func (x Example) Perms() []string { return x.Asks.Requested() }

// Words are the example's permissions in the consent screen's words
// ("runs glab with your GitLab login: anything it wants, as you"); none is "safe".
func (x Example) Words() []string { return x.Asks.Words(x.Perms()) }

// Examples are the folder's mods: one per surface, an agent tool and a
// code host.
var Examples = []Example{
	{"nord", "theme and icon set", Permissions{}},
	{"vim-keys", "keymap", Permissions{}},
	{"context-gauge", "a cell in the sidebar and dashboard, and a card whose rows bind session.clear and session.attach", Permissions{ProjectRead: true, Views: true, Actions: []string{"session.clear"}}},
	{"cost-card", "a dashboard card", Permissions{ProjectRead: true, Views: true}},
	{"notify-me", "OS notifications with core-action buttons, a question's options among them", Permissions{ProjectRead: true, Notify: true, Actions: []string{"question.answer"}}},
	{"thread-timeline", "a full view", Permissions{ProjectRead: true, Views: true}},
	{"standup", "a command: a key, a ≡ menu entry and tm mod run", Permissions{ProjectRead: true, Views: true}},
	{"report-glance", "panel sections: the details panel's and the coordinator's", Permissions{ProjectRead: true, Views: true}},
	{"ci-status", "a section under each project in the sidebar, opening a project-popup tab", Permissions{ProjectRead: true, Views: true}},
	{"changes-column", "a sidebar column of each thread's git changes, before its PR exists", Permissions{ProjectRead: true}},
	{"pr-ci-column", "a sidebar column (also built in)", Permissions{ProjectRead: true}},
	{"memory-tab", "a project-popup tab (also built in)", Permissions{ProjectRead: true, Views: true}},
	{"tasks-tab", "a project-popup tab whose rows bind tm's task actions (built in)", Permissions{ProjectRead: true, Views: true}},
	{"ticker", "a coordinator-panel section or a sidebar section, by its place setting (built in)", Permissions{ProjectRead: true, Views: true}},
	{"memory-search", "an agent tool", Permissions{ProjectRead: true, Tools: true}},
	{"gitlab", "a code host (GitLab, through glab)", Permissions{Commands: []string{"glab"}}},
	{"review-gates", "rules with sections a project's settings switch on: a playbook and its per-project gate", Permissions{}},
	{"zen", "a layout: no sidebar or details panel, only needs and the project", Permissions{}},
	{"cockpit", "a layout: sidebar on the right, details panel along the bottom", Permissions{}},
	{"compact", "a layout: narrower sidebar, one-row footer and status bar, no extra lines", Permissions{}},
}

// ExampleSource is where example id installs from, as tm mod install
// takes it: its folder's tree URL at ExamplesRef. The Mods tab's
// examples list installs it with i.
func ExampleSource(id string) string { return ExamplesURL + "/" + id }

// InstallLines are how example id installs: one tm mod install of
// ExampleSource. A built-in mod's id can't be
// installed (Install refuses it), so for one of those they say to copy
// the folder from a checkout under another id first.
func InstallLines(id string) []string {
	if !IsBundled(id) {
		return []string{"tm mod install " + ExampleSource(id)}
	}
	src := "terminatr/" + ExamplesPath + "/" + id
	cp := "my-" + id
	return []string{
		"built in already; to change it, copy it under another id:",
		"git clone --depth 1 --branch " + ExamplesRef + " " + ExamplesRepo,
		"cp -R " + src + " " + cp + "   # then set id = \"" + cp + "\" in " + cp + "/mod.toml",
		"tm mod install " + cp,
	}
}
