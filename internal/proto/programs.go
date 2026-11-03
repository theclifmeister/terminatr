package proto

// The programs switch (the programs design §3.5): [features] programs in
// config.toml, off by default.

// CapPrograms is in the server's hello only while programs are on: the
// apps draw no program rows, screens or badges without it.
const CapPrograms = "programs"

// MethodProgramsSwitch turns programs on or off (the TUI's Settings →
// all projects → Features: Programs): the server writes the setting,
// then stops the running directors, keeping their records dormant, or
// resumes the dormant ones, each told programs are back on. The
// human's.
const MethodProgramsSwitch = "programs.switch"

// ProgramsSwitchParams are the params of programs.switch.
type ProgramsSwitchParams struct {
	On bool `json:"on"`
}

// ProgramsSwitchResult says what programs.switch did: whether the
// setting changed, and the directors it stopped, resumed and dropped,
// by name ("director acme").
type ProgramsSwitchResult struct {
	Changed bool     `json:"changed"`
	Stopped []string `json:"stopped,omitempty"`
	Resumed []string `json:"resumed,omitempty"`
	Lost    []string `json:"lost,omitempty"`
}

// program.search (the programs design §8): a program's director
// searches its members' histories. The answers are search.query's,
// search.timeline's and search.show's, for a program: the program's own
// index (plain ids) merged with each local member's (ids prefixed
// "<slug>:", as `acme-api:t-0041/report.2#api`), the members' read as a
// thread reads them, without the coordinator's notes and the inbox. A
// member is searched only when its program's search_children is on;
// paired members are not searched. The human and the program's own
// director name it; programs-off while programs are off. The
// server's hello lists CapPrograms for it.
const (
	MethodProgramSearch         = "program.search"
	MethodProgramSearchTimeline = "program.search.timeline"
	MethodProgramSearchShow     = "program.search.show"
)

// ProgramSearchParams are search.query's, for Program, and optionally
// only Member (a member's slug, or the program's own).
type ProgramSearchParams struct {
	Program string   `json:"program"`
	Member  string   `json:"member,omitempty"`
	Words   string   `json:"words"`
	In      []string `json:"in,omitempty"`
	Since   string   `json:"since,omitempty"`
	Until   string   `json:"until,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

// ProgramTimelineParams are search.timeline's, with a prefixed ID.
type ProgramTimelineParams struct {
	Program string `json:"program"`
	ID      string `json:"id"`
	Before  int    `json:"before,omitempty"`
	After   int    `json:"after,omitempty"`
	All     bool   `json:"all,omitempty"`
}

// ProgramShowParams are search.show's, with prefixed IDs.
type ProgramShowParams struct {
	Program string   `json:"program"`
	IDs     []string `json:"ids"`
}

// MethodProgramRename renames a program (tm program rename, the
// programs design §3.4): the server stops its director, renames it
// between two ticker sweeps (rename.Program: the folder, the
// [programs.<slug>] table, links and the members' boards), and starts
// the director again under the new name. The human's; programs-off while
// programs are off.
const MethodProgramRename = "program.rename"

// ProgramRenameParams are the params of program.rename.
type ProgramRenameParams struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ProgramRenameResult says what program.rename did: the program's
// folder now ("" when it had only a table), the director started again
// (its session id, "" when none ran) and what didn't go through after
// the rename stood.
type ProgramRenameResult struct {
	Dir      string   `json:"dir,omitempty"`
	Director string   `json:"director,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}
