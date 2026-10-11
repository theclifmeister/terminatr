package mod

// terminatr's examples/mods folder (mods design §13): one small, working
// mod per mod surface, so people see each surface working and get ideas
// to build and share. They are sample mods, not defaults: nothing in the
// folder is loaded, enabled or bundled. The Mods tab's e, its empty
// state and tm mod examples show the link and each one's install line,
// and do nothing else.

// ExamplesURL is the folder in terminatr's repository.
const ExamplesURL = "https://github.com/theclifmeister/terminatr/tree/main/examples/mods"

// ExamplesRepo is the repository to clone for them.
const ExamplesRepo = "https://github.com/theclifmeister/terminatr"

// ExamplesPath is the folder in a checkout of ExamplesRepo.
const ExamplesPath = "examples/mods"

// Example is one mod of the folder: its id (and folder name), the
// surface it shows and the permissions it asks for.
type Example struct {
	ID, Surface string
	Perms       []string
}

// Examples are the folder's mods, one per surface.
var Examples = []Example{
	{"nord", "theme and icon set", nil},
	{"vim-keys", "keymap", nil},
	{"context-gauge", "a cell in the sidebar and dashboard, with a safe clear action", nil},
	{"cost-card", "a dashboard card", []string{PermProjectRead, PermViews}},
	{"notify-me", "OS notifications", []string{PermProjectRead, PermNotify}},
	{"thread-timeline", "a full view", []string{PermProjectRead, PermViews}},
	{"pr-ci-column", "a sidebar column (also built in)", []string{PermProjectRead}},
	{"memory-tab", "a project-popup tab (also built in)", []string{PermProjectRead, PermViews}},
}

// InstallLine is how example id installs from a checkout of
// ExamplesRepo in the current folder.
func InstallLine(id string) string { return "tm mod install terminatr/" + ExamplesPath + "/" + id }
