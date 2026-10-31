package appapi

import "github.com/theclifmeister/terminatr/internal/proto"

// A project's mods (app API 38; the server's caps say project-mods): the
// project popup's Mods tab, for the Mac app and a paired device.
// mods.list reads, at a device's view level; mods.set and mods.setting
// write, at its write level, signed. Never an agent's: the server's
// mod functions refuse one. Turning a mod on takes the key of the
// consent the app showed (ModToggle.Key), so what the human saw is what
// is granted.

type (
	// ModsListParams are mods.list's: the project, and Consent, a mod
	// whose Toggle should carry its consent's lines.
	ModsListParams struct {
		Project string `json:"project"`
		Consent string `json:"consent,omitempty"`
	}
	// ModsSetParams are mods.set's: mod Mod On or off for Project, Key
	// the key of what its toggle asked (none when it asks nothing).
	ModsSetParams struct {
		Project string `json:"project"`
		Mod     string `json:"mod"`
		On      bool   `json:"on"`
		Key     string `json:"key,omitempty"`
	}
	// ModsSettingParams are mods.setting's: Key of mod Mod for Project
	// to Value, or with none back to all projects' value.
	ModsSettingParams struct {
		Project string    `json:"project"`
		Mod     string    `json:"mod"`
		Key     string    `json:"key"`
		Value   *ModValue `json:"value,omitempty"`
	}
)

// Mods is mods.list's result: every installed mod, for the project.
type Mods struct {
	Project string       `json:"project"`
	Mods    []ProjectMod `json:"mods"`
}

// ProjectMod is one installed mod for the project (proto.ProjectMod):
// its name, what it provides, where it runs in the project's words and
// tone, what turning it on or off does, its settings for the project.
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
	Settings     []ModSetting `json:"settings"`
	SettingsNote string       `json:"settings_note,omitempty"`
	Bad          []string     `json:"bad"`
}

// ModToggle is what turning the mod on or off does: Do (refuse, off,
// limit, off-all, on, consent), what is asked first (Title, Question,
// Consent's lines when asked for), Others, Why, Done and Key.
type ModToggle struct {
	Do       string        `json:"do"`
	Title    string        `json:"title,omitempty"`
	Question string        `json:"question,omitempty"`
	Why      string        `json:"why,omitempty"`
	Done     string        `json:"done,omitempty"`
	Others   []string      `json:"others"`
	Key      string        `json:"key,omitempty"`
	Consent  []ConsentLine `json:"consent"`
}

// ConsentLine is one of a consent's lines: spans, each words in a tone,
// and how it fits a width ("", wrap, cut, hard).
type ConsentLine struct {
	Spans []ConsentSpan `json:"spans"`
	Fit   string        `json:"fit,omitempty"`
}

// ConsentSpan is words in a tone ("", head, faint, good, warn, bad,
// info).
type ConsentSpan struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// ModSetting is one of a mod's settings for the project
// (proto.ModSetting).
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
	Options []string `json:"options"`
	MaxLen  int      `json:"max_len,omitempty"`
}

// ModValue is a setting's value: Type says which of Bool, Int and Text
// ("bool", "int", "text"), "" for none.
type ModValue struct {
	Type string `json:"type"`
	Bool bool   `json:"bool,omitempty"`
	Int  int64  `json:"int,omitempty"`
	Text string `json:"text,omitempty"`
}

// ModsActResult is mods.set's and mods.setting's: what came of it.
type ModsActResult struct {
	Words string `json:"words"`
}

func modsListParams(p ModsListParams) proto.ModsListParams {
	return proto.ModsListParams{Project: p.Project, Consent: p.Consent}
}

func modsSetParams(p ModsSetParams) proto.ModsSetParams {
	return proto.ModsSetParams{Project: p.Project, Mod: p.Mod, On: p.On, Key: p.Key}
}

func modsSettingParams(p ModsSettingParams) proto.ModsSettingParams {
	out := proto.ModsSettingParams{Project: p.Project, Mod: p.Mod, Key: p.Key}
	if p.Value != nil {
		v := proto.ModValue(*p.Value)
		out.Value = &v
	}
	return out
}

func modValueOf(v proto.ModValue) ModValue {
	return ModValue{Type: v.Type, Bool: v.Bool, Int: v.Int, Text: v.Text}
}

func modsOf(r proto.ModsListResult) Mods {
	out := Mods{Project: r.Project, Mods: []ProjectMod{}}
	for _, m := range r.Mods {
		pm := ProjectMod{ID: m.ID, Name: m.Name, Kinds: m.Kinds, Version: m.Version, Description: m.Description, Runs: m.Runs,
			Where: m.Where, WhereTone: m.WhereTone, Own: m.Own, Required: m.Required, Builtin: m.Builtin,
			Settings: []ModSetting{}, SettingsNote: m.SettingsNote, Bad: append([]string{}, m.Bad...)}
		t := m.Toggle
		pm.Toggle = ModToggle{Do: t.Do, Title: t.Title, Question: t.Question, Why: t.Why, Done: t.Done,
			Others: append([]string{}, t.Others...), Key: t.Key, Consent: []ConsentLine{}}
		for _, l := range t.Consent {
			cl := ConsentLine{Fit: l.Fit, Spans: []ConsentSpan{}}
			for _, s := range l.Spans {
				cl.Spans = append(cl.Spans, ConsentSpan{Text: s.Text, Tone: s.Tone})
			}
			pm.Toggle.Consent = append(pm.Toggle.Consent, cl)
		}
		for _, s := range m.Settings {
			pm.Settings = append(pm.Settings, ModSetting{Key: s.Key, Label: s.Label, Type: s.Type, Default: modValueOf(s.Default),
				Value: modValueOf(s.Value), Own: s.Own, HasMin: s.HasMin, Min: s.Min, HasMax: s.HasMax, Max: s.Max,
				Options: append([]string{}, s.Options...), MaxLen: s.MaxLen})
		}
		out.Mods = append(out.Mods, pm)
	}
	return out
}

func modsActOf(r proto.ModsActResult) ModsActResult { return ModsActResult{Words: r.Words} }
