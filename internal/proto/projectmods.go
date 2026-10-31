package proto

// A project's mods, as the project popup's Mods tab shows and changes
// them (docs/SPEC.md §3.8, §4), for a console that isn't the TUI: the
// Mac app and a paired device. The human's only (never an agent's, as
// every mod change); through the app API, mods.list reads at a
// device's view level, mods.set and mods.setting write at its write
// level. The server's hello lists CapProjectMods. This server's
// projects only.
const (
	// MethodModsList lists the installed mods for a project
	// (ModsListParams): a ModsListResult.
	MethodModsList = "mods.list"
	// MethodModsSet turns a mod on or off for a project, as the Mods
	// tab's space does (ModsSetParams): a ModsActResult.
	MethodModsSet = "mods.set"
	// MethodModsSetting sets one of a mod's settings for a project, or
	// has it follow all projects again (ModsSettingParams): a
	// ModsActResult.
	MethodModsSetting = "mods.setting"
)

// CapProjectMods says the server answers mods.list, mods.set and
// mods.setting.
const CapProjectMods = "project-mods"

// ErrModChanged: what mods.set would do, or what it asks first, isn't
// what the console showed (the mod, its consent or the projects changed
// since): nothing changed; list again.
const ErrModChanged = "mod-changed"

// ModsListParams name the project; Consent, a mod's id, has that mod's
// Toggle carry its consent's lines (they are long: one mod at a time).
type ModsListParams struct {
	Project string `json:"project"`
	Consent string `json:"consent,omitempty"`
}

// ModsListResult is mods.list's result: every installed mod, in the
// Mods tab's order.
type ModsListResult struct {
	Project string       `json:"project"`
	Mods    []ProjectMod `json:"mods"`
}

// ProjectMod is one installed mod for the project: its id, name, what
// it provides (Kinds), version and description; Runs, whether it runs
// for the project now; Where, where it runs in the project's words, in
// a tone (modproj tones: "", faint, good, warn, bad); Own, what of its
// settings the project sets ("" without settings); Required (tm can't do
// without it) and Builtin; Toggle, what turning it on or off for the
// project does; Settings, its settings for the project, none when
// SettingsNote says why they can't be set here (built in, or none); Bad,
// the project's values that were refused.
type ProjectMod struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kinds        string       `json:"kinds"`
	Version      string       `json:"version,omitempty"`
	Description  string       `json:"description,omitempty"`
	Runs         bool         `json:"runs,omitempty"`
	Where        string       `json:"where"`
	WhereTone    string       `json:"where_tone,omitempty"`
	Own          string       `json:"own,omitempty"`
	Required     bool         `json:"required,omitempty"`
	Builtin      bool         `json:"builtin,omitempty"`
	Toggle       ModToggle    `json:"toggle"`
	Settings     []ModSetting `json:"settings,omitempty"`
	SettingsNote string       `json:"settings_note,omitempty"`
	Bad          []string     `json:"bad,omitempty"`
}

// ModToggle is what turning the mod on or off for the project does
// (Do: refuse, off, limit, off-all, on, consent; modproj.Toggle) and
// what is asked first: Title and Question (limit, off-all, consent),
// Others (the projects limit leaves it running for), Why (refuse), Done
// (what to say once done). Key is the key of what is asked (modproj.Key),
// which mods.set takes back; Consent, the consent's lines, only for the
// mod mods.list named in Consent.
type ModToggle struct {
	Do       string        `json:"do"`
	Title    string        `json:"title,omitempty"`
	Question string        `json:"question,omitempty"`
	Why      string        `json:"why,omitempty"`
	Done     string        `json:"done,omitempty"`
	Others   []string      `json:"others,omitempty"`
	Key      string        `json:"key,omitempty"`
	Consent  []ConsentLine `json:"consent,omitempty"`
}

// ConsentLine is one of a consent's lines: its spans, each words in a
// tone (head, faint, good, warn, bad, info, "" plain), and Fit, how it
// fits a width ("" as it is, wrap at words, cut with "…", hard: a file
// in full, wrapped anywhere).
type ConsentLine struct {
	Spans []ConsentSpan `json:"spans"`
	Fit   string        `json:"fit,omitempty"`
}

// ConsentSpan is words in a tone.
type ConsentSpan struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// ModSetting is one of a mod's settings for the project: its key,
// label, type (int, bool, string, choice), default, its value for the
// project, Own when the project sets it (else it follows all projects),
// an int's Min and Max (when HasMin, HasMax), a choice's Options, a
// string's MaxLen.
type ModSetting struct {
	Key     string   `json:"key"`
	Label   string   `json:"label,omitempty"`
	Type    string   `json:"type"`
	Default ModValue `json:"default"`
	Value   ModValue `json:"value"`
	Own     bool     `json:"own,omitempty"`
	HasMin  bool     `json:"has_min,omitempty"`
	Min     int64    `json:"min,omitempty"`
	HasMax  bool     `json:"has_max,omitempty"`
	Max     int64    `json:"max,omitempty"`
	Options []string `json:"options,omitempty"`
	MaxLen  int      `json:"max_len,omitempty"`
}

// ModValue is a setting's value: Type says which of Bool, Int and Text
// it is ("bool", "int", "text"; a choice's is text), "" for none.
type ModValue struct {
	Type string `json:"type"`
	Bool bool   `json:"bool,omitempty"`
	Int  int64  `json:"int,omitempty"`
	Text string `json:"text,omitempty"`
}

// ModsSetParams turn mod Mod on (On) or off for project Project. Key is
// the Toggle's key the console showed: when the toggle asks first, it
// must be the key of what it asks now, else ErrModChanged.
type ModsSetParams struct {
	Project string `json:"project"`
	Mod     string `json:"mod"`
	On      bool   `json:"on"`
	Key     string `json:"key,omitempty"`
}

// ModsSettingParams set Key of mod Mod for project Project to Value, or
// with none have it follow all projects again.
type ModsSettingParams struct {
	Project string    `json:"project"`
	Mod     string    `json:"mod"`
	Key     string    `json:"key"`
	Value   *ModValue `json:"value,omitempty"`
}

// ModsActResult says what came of it, as the TUI's footer does.
type ModsActResult struct {
	Words string `json:"words"`
}
