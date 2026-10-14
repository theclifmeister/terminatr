package proto

import "github.com/theclifmeister/terminatr/internal/widget"

// Mod cells (docs/SPEC.md §5.3, Cells; mods design §4 and §6): what code
// mods publish into their columns and the status bar. The server keeps
// the latest cell per mod, column and target and sends them with the rows
// it already sends, since protocol 13: session.list's Mods (the columns,
// the cells of projects, tasks and threads, the status segments), a
// session's own cells on its SessionInfo, and a project's on its
// ProjectWatch. Consoles draw them with their own theme, icons and
// widths; a cell is spans, never terminal bytes.

// Column slots: where a mod column shows (the manifest's where).
const (
	SlotSidebarProject     = "sidebar.project"
	SlotSidebarCoordinator = "sidebar.coordinator"
	SlotSidebarThread      = "sidebar.thread"
	SlotDashboardTask      = "dashboard.task"
	SlotDashboardThread    = "dashboard.thread"
)

// CellTarget is what a cell is about, one of: a project (Project alone),
// a task or a thread of a project (Project and Task, or Project and
// Thread), or a session (Session alone).
type CellTarget struct {
	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Session string `json:"session,omitempty"`
}

// ModCell is a mod's cell: its spans in its column Column ("age", the
// column's own id) for Target.
type ModCell struct {
	Mod    string        `json:"mod"`
	Column string        `json:"column"`
	Target CellTarget    `json:"target"`
	Spans  []widget.Span `json:"spans"`
}

// ModColumn is an enabled code mod's column as its manifest declares it:
// ID is "<mod>.<column>", the id layouts and the sidebar's order name it
// by; Where are its slots.
type ModColumn struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Width int      `json:"width"`
	Where []string `json:"where"`
}

// ModStatus is a mod's segment of the status bar, Name its mod's name.
type ModStatus struct {
	Mod   string        `json:"mod"`
	Name  string        `json:"name,omitempty"`
	Spans []widget.Span `json:"spans"`
}

// ModToast is a short message a mod showed with $.ui.toast: Seq counts
// the server's toasts, so a console shows each once; Name is its mod's
// name, Level info, warn or error. Text is one line, cleaned by the
// server.
type ModToast struct {
	Seq   uint64 `json:"seq"`
	Mod   string `json:"mod"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text"`
	Level string `json:"level"`
}

// ModCells are the mods' cells as session.list sends them: the enabled
// code mods' columns, in mod order, then the cells of projects, tasks
// and threads (a session's are on its SessionInfo) and the status
// segments, of the mods whose hosts haven't failed, and the toasts of
// the last few seconds. Sections are the sidebar sections the consoles
// show (mod API 1.7), so an attached console knows whether to ask
// mod.views for them.
type ModCells struct {
	Columns  []ModColumn  `json:"columns,omitempty"`
	Cells    []ModCell    `json:"cells,omitempty"`
	Status   []ModStatus  `json:"status,omitempty"`
	Toasts   []ModToast   `json:"toasts,omitempty"`
	Sections []ModSection `json:"sections,omitempty"`
}

// ModSection is a mod's sidebar section as its manifest declares it:
// Slot is sidebar.section or sidebar.project.section. In safe mode only
// the built-in mods' are listed.
type ModSection struct {
	Mod     string `json:"mod"`
	View    string `json:"view"`
	Title   string `json:"title"`
	Slot    string `json:"slot"`
	Builtin bool   `json:"builtin,omitempty"`
}
