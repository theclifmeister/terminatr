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
