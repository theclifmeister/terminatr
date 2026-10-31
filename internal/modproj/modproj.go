// Package modproj is a project's mods as the project popup's Mods tab
// shows them (docs/SPEC.md §4), in words any console draws: where each
// mod runs for the project, what turning it on or off there does and
// what is asked first, and the mod's consent, line by line. The TUI
// draws them; the server sends them to the Mac app and paired devices
// (mods.list, docs/SPEC.md §3.8), so every console asks the same.
package modproj

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Tones a console draws words in: the TUI's theme styles.
const (
	Plain = ""
	Head  = "head"
	Faint = "faint"
	Good  = "good"
	Warn  = "warn"
	Bad   = "bad"
	Info  = "info"
)

// oneLine is s on one line, plain text (the TUI's oneLine).
func oneLine(s string) string { return textsafe.Clean(s, false) }

// Name is the mod's name, its id without one.
func Name(md mod.Mod) string {
	if md.Manifest != nil && strings.TrimSpace(md.Manifest.Name) != "" {
		return oneLine(mod.Clean(md.Manifest.Name, false))
	}
	return md.ID
}

// Kinds is what the mod provides, "built in" last for a bundled one.
func Kinds(md mod.Mod) string {
	var k []string
	if md.Manifest != nil {
		k = md.Manifest.Kinds()
	}
	if md.Builtin {
		k = append(k, "built in")
	}
	if len(k) == 0 {
		return "-"
	}
	return strings.Join(k, " · ")
}

// JoinAnd is "a, b and c".
func JoinAnd(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// Where is where mod md runs, in project slug's words, and its tone: on
// or off for it, or following all projects, with what all projects do.
func Where(md mod.Mod, slug string) (string, string) {
	on := md.State == mod.Enabled
	switch {
	case md.State == mod.Invalid || md.State == mod.Incompatible:
		return md.Status(), Bad
	case len(md.Projects) == 0 && on:
		return "follows all projects: on", Plain
	case len(md.Projects) == 0:
		return "follows all projects: off", Faint
	case slices.Contains(md.Projects, slug) && on:
		return "on for this project", Good
	case slices.Contains(md.Projects, slug):
		return "on for this project, but the mod is off", Warn
	case on:
		return "off for this project · runs for " + JoinAnd(md.Projects), Faint
	}
	return "off for this project", Faint
}

// OwnSettings is what of mod md's settings project slug sets, "" for a
// mod without settings.
func OwnSettings(md mod.Mod, slug string) string {
	if md.Manifest == nil || len(md.Manifest.Settings) == 0 {
		return ""
	}
	switch n := len(md.ProjectSettings[slug]); n {
	case 0:
		return "settings: all projects"
	case 1:
		return "settings: 1 of its own"
	default:
		return fmt.Sprintf("settings: %d of its own", n)
	}
}

// What turning a mod on or off for a project does (Toggle.Do).
const (
	// DoRefuse: nothing; Why says why.
	DoRefuse = "refuse"
	// DoOff: off for the project, as tm mod disable --project does.
	DoOff = "off"
	// DoLimit: the mod runs for every project; after Question it runs
	// only for Others (mod.SetProjects).
	DoLimit = "limit"
	// DoOffAll: the mod runs for every project and this is the only
	// one; after Question it is turned off.
	DoOffAll = "off-all"
	// DoOn: on for the project, as tm mod enable --project does; it
	// already runs for others and wants nothing more, so nothing is
	// asked.
	DoOn = "on"
	// DoConsent: on for the project after the mod's consent (Consent,
	// Question): it is off, or wants more.
	DoConsent = "consent"
)

// Toggle is what turning mod md on or off for project slug does, and
// what a console asks first: the Mods tab's space.
type Toggle struct {
	Do string
	// Title and Question are what is asked first (DoLimit, DoOffAll,
	// DoConsent's Question); Why is DoRefuse's reason.
	Title, Question, Why string
	// Others are the projects DoLimit leaves it running for.
	Others []string
	// Done is what a console says once it is done.
	Done string
}

// ToggleFor is what turning mod md on or off for project slug does;
// locals are this server's projects.
func ToggleFor(md mod.Mod, slug string, locals []string) Toggle {
	name := Name(md)
	switch {
	case md.State == mod.Invalid || md.State == mod.Incompatible:
		return Toggle{Do: DoRefuse, Why: name + " can't be turned on: " + md.Reason()}
	case md.Runs(slug) && md.Required:
		return Toggle{Do: DoRefuse, Why: mod.ErrRequired{ID: md.ID}.Error()}
	case md.Runs(slug) && len(md.Projects) == 0:
		others := slices.DeleteFunc(slices.Clone(locals), func(p string) bool { return p == slug })
		if len(others) == 0 {
			return Toggle{Do: DoOffAll, Title: "Turn " + name + " off",
				Question: name + " runs for every project, and " + slug + " is the only one. Turn it off?", Done: name + " off"}
		}
		return Toggle{Do: DoLimit, Title: "Turn " + name + " off for " + slug, Others: others,
			Question: name + " runs for every project. Run it only for " + JoinAnd(others) + " instead? A project made later won't run it until you turn it on there.",
			Done:     name + " off for " + slug}
	case md.Runs(slug):
		return Toggle{Do: DoOff, Done: name + " off for " + slug}
	case md.State == mod.Enabled && len(md.WantsMore) == 0:
		// Running it for one more project grants nothing new.
		return Toggle{Do: DoOn, Done: name + " on for " + slug}
	}
	t := Toggle{Do: DoConsent, Title: name, Question: ConsentQuestion(md, slug), Done: name + " on for " + slug}
	if md.State == mod.Enabled {
		t.Done = name + " has what it asked for"
	}
	return t
}

// ConsentQuestion is the consent's question, for project slug ("" for
// all projects).
func ConsentQuestion(md mod.Mod, slug string) string {
	if md.State == mod.Enabled {
		return "Grant " + Name(md) + " what it wants more?"
	}
	if slug != "" {
		return "Enable " + Name(md) + " for " + JoinAnd(EnabledFor(md, slug)) + "?"
	}
	return "Enable " + Name(md) + "?"
}

// EnabledFor are the projects mod md, off, runs for once tm mod enable
// --project slug turns it on: those it was limited to, then slug.
func EnabledFor(md mod.Mod, slug string) []string {
	if slices.Contains(md.Projects, slug) {
		return md.Projects
	}
	return append(slices.Clone(md.Projects), slug)
}

// How a consent line fits a console's width (Line.Fit).
const (
	// FitNone: as it is.
	FitNone = ""
	// FitWrap: wrapped at words.
	FitWrap = "wrap"
	// FitCut: cut at the width with "…".
	FitCut = "cut"
	// FitHard: wrapped anywhere, its own lines kept (a file in full).
	FitHard = "hard"
)

// Span is words in a tone.
type Span struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// Line is one of the consent's lines: its spans, and how it fits.
type Line struct {
	Spans []Span `json:"spans"`
	Fit   string `json:"fit,omitempty"`
}

// Text is the line's words.
func (l Line) Text() string {
	var b strings.Builder
	for _, s := range l.Spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func line(fit string, spans ...Span) Line { return Line{Spans: spans, Fit: fit} }

func plain(fit, text string) Line { return line(fit, Span{Text: text}) }

// PermTone is permission perm's tone: what it reaches.
func PermTone(perm string) string {
	switch perm {
	case mod.PermTools, mod.PermFiles, mod.PermNet:
		return Warn
	case mod.PermCommands:
		return Bad
	}
	return Info
}

// Consent is mod md's consent as tm asks it, enabling or installing it
// (mods design §9), then question: what it is and provides, what it
// changes in what agents run or read, each permission it asks for in
// plain words and its tone (" · new" after one an enabled mod wants
// more), the defaults it suggests, every rules addendum, agent manifest,
// task template, tool and guard rule in full.
func Consent(md mod.Mod, question string) []Line {
	man := md.Manifest
	if man == nil {
		return []Line{line(FitNone, Span{Text: question, Tone: Head})}
	}
	var out []Line
	out = append(out, line(FitCut, Span{Text: oneLine(man.Headline()), Tone: Head}))
	if d := strings.TrimSpace(oneLine(mod.Clean(man.Description, false))); d != "" {
		out = append(out, plain(FitWrap, d))
	}
	if man.Homepage != "" {
		out = append(out, line(FitCut, Span{Text: oneLine(mod.Clean(man.Homepage, false)), Tone: Faint}))
	}
	out = append(out, plain(FitWrap, "Provides: "+strings.Join(man.Kinds(), " · ")))
	if v := man.ViewWords(); len(v) > 0 {
		out = append(out, plain(FitWrap, "Panels it draws: "+strings.Join(v, " · ")))
	}
	if k := man.Provides.CodeHost; k != "" {
		out = append(out, plain(FitWrap, fmt.Sprintf("Code host %s: for projects whose code_host is %q, tm asks it about their pull requests and has it merge one tm thread merge checked.", k, k)))
	}
	if c := man.CommandWords(); len(c) > 0 {
		out = append(out, plain(FitWrap, "Commands you can run: "+strings.Join(c, " · ")))
	}
	out = append(out, plain(FitNone, ""))
	changes := md.AgentChanges()
	if len(changes) > 0 {
		out = append(out, plain(FitNone, "It changes what agents run or read:"))
		for _, c := range changes {
			out = append(out, plain(FitWrap, "  • "+c.Long))
		}
	}
	asked := man.Permissions.Requested()
	switch {
	case len(asked) == 0 && len(changes) > 0:
		out = append(out, plain(FitNone, "It asks for no permission."))
	case len(asked) == 0:
		out = append(out, line(FitNone, Span{Text: "It asks for no permission: "}, Span{Text: "safe", Tone: Good}, Span{Text: "."}))
		out = append(out, line(FitWrap, Span{Text: "Its own data, columns, settings and log, and events about sessions and the clock.", Tone: Faint}))
	default:
		out = append(out, plain(FitNone, "It may:"))
		for _, p := range asked {
			l := line(FitCut, Span{Text: "  • "}, Span{Text: oneLine(man.Permissions.PermWords(p)), Tone: PermTone(p)})
			if md.State == mod.Enabled && slices.Contains(md.WantsMore, p) {
				l.Spans = append(l.Spans, Span{Text: " · new", Tone: Faint})
			}
			out = append(out, l)
		}
	}
	if d := man.DefaultWords(); len(d) > 0 {
		out = append(out, plain(FitNone, ""))
		out = append(out, plain(FitWrap, "It suggests defaults for all projects. Nothing changes until you apply them (d in the Mods tab, once it's on), and a setting your config.toml sets stays yours:"))
		for _, l := range d {
			out = append(out, plain(FitCut, "  • "+l))
		}
	}
	if shown := md.Shown(); shown != "" {
		out = append(out, plain(FitNone, ""))
		out = append(out, plain(FitHard, shown))
	}
	return append(out, plain(FitNone, ""), line(FitNone, Span{Text: question, Tone: Head}))
}

// Key is what a console shows for a question, as a short hash: the
// consent's lines and question, or a question alone. A change of the
// mod, or of what the question names, changes it, so the server turns a
// mod on only for the key of what the human saw (mods.set).
func Key(lines []Line, question string) string {
	b, _ := json.Marshal(struct {
		Lines    []Line `json:"lines"`
		Question string `json:"question"`
	}{lines, question})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// Asked is what the human must have seen to do t for mod md: the
// consent's lines (DoConsent) and the question; nil and "" when nothing
// is asked.
func Asked(md mod.Mod, t Toggle) ([]Line, string) {
	switch t.Do {
	case DoConsent:
		return Consent(md, t.Question), t.Question
	case DoLimit, DoOffAll:
		return nil, t.Question
	}
	return nil, ""
}

// KeyOf is the key of what t asks first, "" when nothing is asked.
func KeyOf(md mod.Mod, t Toggle) string {
	lines, q := Asked(md, t)
	if q == "" {
		return ""
	}
	return Key(lines, q)
}
