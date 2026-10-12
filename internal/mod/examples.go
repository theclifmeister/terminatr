package mod

// terminatr's examples/mods folder (mods design §13): one small, working
// mod per mod surface, so people see each surface working and get ideas
// to build and share. They are sample mods, not defaults: nothing in the
// folder is loaded or enabled, though three of them are the sources of
// built-in mods. The Mods tab's e, its empty state and tm mod examples
// show the link and each one's install lines, and do nothing else.

// ExamplesURL is the folder in terminatr's repository.
const ExamplesURL = "https://github.com/theclifmeister/terminatr/tree/main/examples/mods"

// ExamplesRepo is the repository to clone for them.
const ExamplesRepo = "https://github.com/theclifmeister/terminatr"

// ExamplesPath is the folder in a checkout of ExamplesRepo.
const ExamplesPath = "examples/mods"

// Example is one mod of the folder: its id (and folder name), the
// surface it shows and the permissions its mod.toml asks for.
type Example struct {
	ID, Surface string
	Asks        Permissions
}

// Perms are the permissions the example asks for, by name.
func (x Example) Perms() []string { return x.Asks.Requested() }

// Words are the example's permissions in the consent screen's words
// ("runs glab"); none is "safe".
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
	{"pr-ci-column", "a sidebar column (also built in)", Permissions{ProjectRead: true}},
	{"memory-tab", "a project-popup tab (also built in)", Permissions{ProjectRead: true, Views: true}},
	{"tasks-tab", "a project-popup tab whose rows bind tm's task actions (built in)", Permissions{ProjectRead: true, Views: true}},
	{"memory-search", "an agent tool", Permissions{ProjectRead: true, Tools: true}},
	{"gitlab", "a code host (GitLab, through glab)", Permissions{Commands: []string{"glab"}}},
}

// InstallLines are how example id installs from a checkout of
// ExamplesRepo in the current folder. A built-in mod's id can't be
// installed (Install refuses it), so for one of those they say to copy
// the folder under another id first.
func InstallLines(id string) []string {
	src := "terminatr/" + ExamplesPath + "/" + id
	if !IsBundled(id) {
		return []string{"tm mod install " + src}
	}
	cp := "my-" + id
	return []string{
		"built in already; to change it, copy it under another id:",
		"cp -R " + src + " " + cp + "   # then set id = \"" + cp + "\" in " + cp + "/mod.toml",
		"tm mod install " + cp,
	}
}
