package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/theme"
)

// ManifestVersion is the manifest format this binary reads.
const ManifestVersion = 1

// Manifest is the declarative definition of an agent. The format is
// documented in docs/SPEC.md §8.2; manifests/claude.toml is the reference.
type Manifest struct {
	ManifestVersion int    `toml:"manifest_version"`
	Name            string `toml:"name"`
	Display         string `toml:"display"`
	// RoleFiles are the names, besides AGENTS.md, under which the
	// coordinator's role file is linked in the project folder for this
	// agent to read (Claude Code: "CLAUDE.md").
	RoleFiles []string `toml:"role_files"`

	Identify struct {
		Argv0       []string `toml:"argv0"`        // basenames of argv[0] after unwrapping
		VersionArgs []string `toml:"version_args"` // e.g. ["--version"], for tm doctor
		// MinVersion is the oldest agent version terminatr's generated
		// files work with; tm doctor warns below it, naming
		// MinVersionWhy. Empty: no minimum.
		MinVersion    string `toml:"min_version"`
		MinVersionWhy string `toml:"min_version_why"`
		// LastTested is the newest agent version this manifest was
		// checked against: information only (tm doctor, tm agent list,
		// the session log), never a gate. A newer version is supported
		// until something actually breaks (docs/SPEC.md §8.8).
		LastTested string `toml:"last_tested"`
	} `toml:"identify"`

	Launch struct {
		Command     string            `toml:"command"`
		Args        []string          `toml:"args"`         // templates; empty results are dropped
		KickoffArgs []string          `toml:"kickoff_args"` // appended last when Kickoff is set; start with "--" if the CLI has variadic flags
		ResumeArgs  []string          `toml:"resume_args"`  // appended when Resume is set
		YoloArgs    []string          `toml:"yolo_args"`
		ModelArgs   []string          `toml:"model_args"` // appended when Model is set
		Env         map[string]string `toml:"env"`        // values are templates; an empty result sets nothing
		UnsetEnv    []string          `toml:"unset_env"`  // inherited variables to drop; trailing * = prefix
		Files       []ManifestFile    `toml:"files"`
	} `toml:"launch"`

	Inject struct {
		Prompt Injector `toml:"prompt"`
		// EmptyRule names a screen rule that matches only while the
		// prompt box is empty. The paste injector waits for it, so it
		// never appends to a restored or half-typed prompt.
		EmptyRule string `toml:"empty_rule"`
		// TokenEnv names a variable of the hook's environment that tm hook
		// hands to the server with every event, for the prompt channel
		// (PromptTarget.Token): Claude's messaging token. The server
		// keeps it in memory only and never logs it.
		TokenEnv string `toml:"token_env"`
		// Clear is the prompt that clears the conversation (Claude's
		// /clear), pasted like any prompt: the ticker's auto-clear of an
		// idle coordinator (auto_clear) sends it. Empty: the agent has
		// none, and auto-clear leaves its sessions alone.
		Clear string `toml:"clear"`
		// SwitchModel is the in-session command that switches the
		// running agent's model, keeping its conversation ("/model
		// {model}"), pasted like any prompt once the agent is idle with
		// an empty prompt box: a coordinator's coordinator_model change
		// applies live (docs/SPEC.md §8.2, Models). {model} is the
		// model's name, or SwitchModelDefault for the agent's own
		// default. Empty: the agent can't switch, so the change applies
		// on its next start; so does a switch to its own default without
		// SwitchModelDefault.
		SwitchModel        string `toml:"switch_model"`
		SwitchModelDefault string `toml:"switch_model_default"`
		// Interrupt is the key that stops the agent's running turn, for
		// a prompt sent with tm thread prompt --interrupt (docs/SPEC.md
		// §8.6, Priority prompts). Empty: the agent has none, and such a
		// prompt is steered into the turn instead.
		Interrupt string `toml:"interrupt"`
		// InterruptPaste says when that prompt is pasted: "before" the
		// key, for an agent that sends what was typed mid-turn once
		// interrupted (Claude), or "after" it once the agent is idle
		// with an empty box (Codex: a steer pasted first is left
		// unanswered). Empty means "after".
		InterruptPaste string `toml:"interrupt_paste"`
	} `toml:"inject"`

	// RemoteControl: reaching the session from another device, e.g.
	// Claude Code's Remote Control (docs/SPEC.md §8.2). Without args the
	// agent has none.
	RemoteControl RemoteControl `toml:"remote_control"`

	// Answer: how a question menu takes the user's answer, which the
	// coordinator relays with `tm thread answer` (docs/SPEC.md §11.2).
	// Without a rule the agent's menus are answered in its pane only.
	Answer Answer `toml:"answer"`

	// ListModels is how tm asks the installed agent for the models the
	// user can use (docs/SPEC.md §8.2, Models); nil: the agent can't
	// tell, so threads run its own default. It needs model_args.
	ListModels *ModelLister `toml:"list_models"`
	// LegacyModels is the [[models]] list older manifests carried: read
	// so they still load, never used (tm doctor says so).
	LegacyModels []map[string]any `toml:"models"`

	// Guard: the agent's own paths the guard knows (docs/SPEC.md §8.6).
	Guard Guard `toml:"guard"`

	// Theme is the theme file tm keeps in the agent's own config folder
	// for a running agent to reload when the tm theme changes (Claude's
	// <config>/themes, docs/SPEC.md §8.6): the agent's Go code knows the
	// folder and writes it (a LiveThemer). Empty: none.
	Theme ManifestFile `toml:"theme"`

	Screen struct {
		// Resize: "follow" (the default) lets the console typed in size
		// the pane; "explicit" resizes it only on a window resize or a
		// split change, for inline renderers that garble their scrollback
		// on every resize (docs/SPEC.md §3.3).
		Resize Resize `toml:"resize"`
	} `toml:"screen"`

	// IgnoreFields: a hook event with any of these payload fields present
	// and non-empty (e.g. Claude's agent_id on subagent events) is ignored
	// for state, session id and todos. It still feeds [[hooks]] entries
	// that have a counter, and is answered as any other (Respond): the
	// guard judges a subagent's tool calls too, and marks its escapes.
	IgnoreFields []string  `toml:"ignore_fields"`
	Hooks        []HookMap `toml:"hooks"`
	Todos        []TodoMap `toml:"todos"`
	Rules        []Rule    `toml:"rules"`

	// Sources holds tested_versions, session_field, [hook], [status_file],
	// [jsonl_tail] and [todos_snapshot].
	Sources
}

// RemoteControl is a manifest's [remote_control] table. Templates see the
// LaunchSpec; .RemoteName is the name the remote side lists the session
// under (the project's slug for a coordinator).
type RemoteControl struct {
	// Args are appended (before the kickoff) when it starts on.
	Args []string `toml:"args"`
	// Enable and Disable are in-session text, pasted as a prompt, that
	// turn it on or off in the running session (e.g. a slash command).
	// Empty: the session is restarted, resumed, with or without Args.
	Enable  string `toml:"enable"`
	Disable string `toml:"disable"`
	// DisableDialog answers a dialog the disable text opens.
	DisableDialog *RemoteDialog `toml:"disable_dialog"`
	// StatusField names a [status_file] fields entry that is non-empty
	// while remote control is on: the observed state, which wins over
	// what tm last asked for (an agent may reconnect on resume). Without
	// that entry (or a status file) tm goes by what it asked for.
	StatusField string `toml:"status_field"`
}

// Answer is a manifest's [answer] table. A menu's options are numbered;
// option N is chosen by typing its number. The option named TextOption
// takes the user's own words: its number focuses it, then the text is
// typed and Submit sent. When the option needs a key to open its text
// field (Codex: Tab for notes), TextKey is sent between the two. Where a
// number submits the option at once (Codex), TextFocus moves to the text
// option instead: its key, typed N-1 times from the first option.
type Answer struct {
	// Rule names the screen rule that matches a question menu; tm
	// answers only while it is the screen's settled match.
	Rule       string `toml:"rule"`
	TextOption string `toml:"text_option"` // part of the free-text option's label
	TextFocus  string `toml:"text_focus"`  // key that moves down one option, in place of the number, e.g. "\x1b[B"
	TextKey    string `toml:"text_key"`    // keys after the option's number that open its text field, e.g. "\t"
	Submit     string `toml:"submit"`      // keys after the text, e.g. "\r"
}

// Guard is a manifest's [guard] table. Each entry is a path relative to
// home ("~/" optional) or under an environment variable ("$CODEX_HOME/auth.json").
type Guard struct {
	// Secrets are the agent's credential stores. The credentials rule
	// covers every known agent's, whatever the session's agent.
	Secrets []string `toml:"secrets"`
	// Writable are the agent's own folders its threads write to besides
	// the worktree and the temporary folders (the worktree-only rule).
	Writable []string `toml:"writable"`
	// Temp is where the agent's sandbox has it write temporary files
	// (its TMPDIR): entries absolute or under $VAR, "{uid}" the user's
	// id; the first that resolves is the agent's temp folder, the only
	// one an agent's file arguments may name besides its worktree
	// (docs/SPEC.md §7.2).
	Temp []string `toml:"temp"`
	// TempPrivate are subfolders of the temp folder that the agent's
	// sandbox keeps from it (Claude's bash-edit-diff): no file argument
	// of its may be in them.
	TempPrivate []string `toml:"temp_private"`
	// PromptsEscape: every permission prompt the agent shows a thread
	// asks to run outside its sandbox (Codex's escalations under -a
	// on-request), so the user answers it, never the coordinator.
	PromptsEscape bool `toml:"prompts_escape"`
	// EscapeRule is the screen rule whose match is a permission dialog
	// for a command run outside the sandbox: an escape, the user's to
	// answer, whatever the hooks said.
	EscapeRule string `toml:"escape_rule"`
	// TempScratch: the temp folder is shared by all the agent's sessions
	// of the user, each keeping its working folder's files in a folder
	// named after the folder's path (Claude Code's scratchpad). A file
	// argument may then be only in the caller's own, never loose in the
	// temp folder nor in another's (docs/SPEC.md §7.2, T586).
	TempScratch bool `toml:"temp_scratch"`
	// TempMax is the longest temp folder, in bytes, the agent's sandbox
	// keeps its TMPDIR in; a longer one it leaves for a shared folder
	// (Claude's: /tmp/claude-<uid>). 0: no limit.
	TempMax int `toml:"temp_max"`
	// Tools are the agent's tools the guard judges, by name: each one's
	// kind (shell, write, patch, read, glob) and the input fields that
	// kind reads, e.g. Bash = { kind = "shell", fields = ["command"] };
	// a shell tool whose commands are PowerShell says syntax =
	// "powershell". A tool not listed is not judged.
	Tools map[string]guard.Tool `toml:"tools"`
}

// GuardPaths resolves [guard] entries to absolute paths, against home
// and getenv. An entry whose variable is unset or empty, or a relative
// one without a home, is left out.
func GuardPaths(entries []string, home string, getenv func(string) string) []string {
	var out []string
	for _, e := range entries {
		missing := false
		p := os.Expand(e, func(name string) string {
			v := getenv(name)
			if v == "" {
				missing = true
			}
			return v
		})
		p = strings.TrimPrefix(p, "~/")
		if missing || p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			if home == "" {
				continue
			}
			p = filepath.Join(home, p)
		}
		out = append(out, filepath.Clean(p))
	}
	return out
}

// GuardTemp resolves [guard] temp to the agent's temp folder: the first
// entry whose variables are set and that is absolute on this system,
// with "{uid}" the user's id (an entry with one is left out where there
// are no uids: Windows); "" for none. An entry that resolves to the root
// or to a folder holding home is skipped: it is no agent's own.
func GuardTemp(entries []string, home string, getenv func(string) string) string {
	if all := GuardTemps(entries, home, getenv); len(all) > 0 {
		return all[0]
	}
	return ""
}

// GuardTemps is every [guard] temp entry that resolves as GuardTemp
// says, in order: the agent's sandbox may have set any of its variables
// when it started, so each is a folder it may write.
func GuardTemps(entries []string, home string, getenv func(string) string) []string {
	var out []string
	for _, e := range entries {
		missing := false
		p := os.Expand(e, func(name string) string {
			v := getenv(name)
			if v == "" {
				missing = true
			}
			return v
		})
		if strings.Contains(p, "{uid}") {
			if os.Getuid() < 0 {
				continue
			}
			p = strings.ReplaceAll(p, "{uid}", strconv.Itoa(os.Getuid()))
		}
		if p = filepath.Clean(p); missing || !filepath.IsAbs(p) || filepath.Dir(p) == p || holds(p, home) {
			continue
		}
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// holds reports whether dir is home or holds it (home "" holds nothing).
func holds(dir, home string) bool {
	if home == "" {
		return false
	}
	_, ok := fsx.Within(dir, filepath.Clean(home))
	return ok
}

// validGuardPath reports whether a [guard] entry is home-relative or
// starts with a variable, and stays out of "..".
func validGuardPath(e string) bool {
	if e == "" || slices.Contains(strings.Split(filepath.ToSlash(e), "/"), "..") {
		return false
	}
	return !filepath.IsAbs(e) || strings.HasPrefix(e, "$")
}

// AnswerOf returns a's [answer], or nil when it has none.
func AnswerOf(a Agent) *Answer {
	if m := ManifestOf(a); m != nil && m.Answer.Rule != "" {
		return &m.Answer
	}
	return nil
}

// RemoteDialog answers a dialog that in-session text opens: once the
// screen contains Contains, Keys are typed, and the change counts as made
// once the screen contains Done.
type RemoteDialog struct {
	Contains string `toml:"contains"`
	Keys     string `toml:"keys"`
	Done     string `toml:"done"`
}

// Supported reports whether the agent has remote control at all.
func (r *RemoteControl) Supported() bool { return r != nil && len(r.Args) > 0 }

// Text is the in-session text that turns it on or off, rendered for
// spec; "" means the session has to be restarted instead.
func (r *RemoteControl) Text(on bool, spec LaunchSpec) (string, error) {
	t := r.Disable
	if on {
		t = r.Enable
	}
	if t == "" {
		return "", nil
	}
	return render(t, spec)
}

// ObservesRemote reports whether the status file says if remote control
// is on (remote_control.status_field names one of its fields).
func (m *Manifest) ObservesRemote() bool {
	f := m.RemoteControl.StatusField
	return m.RemoteControl.Supported() && f != "" && m.StatusFile != nil && m.StatusFile.Fields[f] != ""
}

// RemoteControlOf returns a's [remote_control], or nil when a has none.
func RemoteControlOf(a Agent) *RemoteControl {
	if m := ManifestOf(a); m != nil && m.RemoteControl.Supported() {
		return &m.RemoteControl
	}
	return nil
}

// ClearTextOf is the prompt that clears a's conversation ([inject]
// clear), "" when it has none.
func ClearTextOf(a Agent) string {
	if m := ManifestOf(a); m != nil {
		return m.Inject.Clear
	}
	return ""
}

// ManifestFile is a generated file written into the session's runtime dir
// before launch (a hook plugin, an extension, a settings file).
type ManifestFile struct {
	Path     string `toml:"path"`
	Template string `toml:"template"`
}

// HookMap maps one harness event to a signal. Every matching entry
// applies, so use negated matches ("!value") to keep entries exclusive.
type HookMap struct {
	Event     string            `toml:"event"`
	Match     map[string]string `toml:"match"` // see matches for the pattern syntax
	State     State             `toml:"state"` // empty: no state change
	Reason    string            `toml:"reason"`
	Transient bool              `toml:"transient"`
	// Counter is "+name" or "-name" (background activity started or
	// ended), keyed by the payload field CounterKey.
	Counter    string `toml:"counter"`
	CounterKey string `toml:"counter_key"`
	// ClearsContext says the event leaves the conversation's context
	// empty or rewritten (/clear, a compaction): the server forgets the
	// session's context use until the next request reports it
	// (docs/SPEC.md §8.6, Context).
	ClearsContext bool `toml:"clears_context"`
	// Command names the session command the event stands for, when the
	// user typed it: a slash command the agent offers without a mod of
	// its own (CommandSaveClear, CommandAnswer). The server runs it and
	// Respond's .Command is what came of it, "" when it didn't run (the
	// session can't).
	Command string `toml:"command"`
	// Respond is a template printed back to the harness. It sees .Event,
	// .Payload, .Context (rendered only when the template uses it) and
	// .Guard (the guard's refusal of the payload's tool call, or nil;
	// judged only when the template uses it).
	Respond string `toml:"respond"`
	// Timeout is how long tm hook waits for the response: "" (the
	// default, 500 ms), "context" (3 s, for the event whose response
	// carries the session's context), "command" (3 s, sent once, for a
	// Command's entry) or "guard" (HookGuardWait, for the entry whose
	// response is the guard's verdict: without one, tm hook answers with
	// guard.Unchecked, FailClosed). See HookTimeout.
	Timeout string `toml:"timeout"`
}

// Matches reports whether the entry applies to the hook event (event,
// payload).
func (h HookMap) Matches(event string, payload map[string]any) bool {
	return h.Event == event && matches(h.Match, payload)
}

// ClearsContext reports whether agent a's manifest says the hook event
// (event, payload) clears the conversation's context use.
func ClearsContext(a Agent, event string, payload map[string]any) bool {
	m := ManifestOf(a)
	if m == nil {
		return false
	}
	for _, h := range m.Hooks {
		if h.ClearsContext && h.Event == event && matches(h.Match, payload) {
			return true
		}
	}
	return false
}

// Session commands a [[hooks]] entry may stand for (HookMap.Command):
// a coordinator's save and clear (tm clear) and the opening of its
// questions to the user (tm ask open): Claude's /saveclear and /answer
// (docs/SPEC.md §8.6).
const (
	CommandSaveClear = "saveclear"
	CommandAnswer    = "answer"
)

// Hook timeouts a [[hooks]] entry may ask for.
const (
	HookTimeoutResponse = "response"
	HookTimeoutContext  = "context"
	HookTimeoutCommand  = "command"
	HookTimeoutGuard    = "guard"
)

// HookGuardWait is how long tm hook waits, in all, for the guard's
// verdict on a tool call (timeout = "guard"): longer than a plain
// response, as the server judges the call first, and well inside the
// agents' 5 s hook timeout, so the refusal tm hook gives without a
// verdict is always read.
const HookGuardWait = 3 * time.Second

// HookContextWait is how long tm hook waits, in all, for the response to
// an event whose entry has timeout = "context": the server counts a
// context as given only when it answered well within it.
const HookContextWait = 3 * time.Second

// RendersAccess reports whether the manifest turns the access policy
// (LaunchSpec.Access) into the harness's own settings. `tm agent list`
// marks agents that don't as unenforced (docs/SPEC.md §8.7).
func (m *Manifest) RendersAccess() bool {
	for _, f := range m.Launch.Files {
		if strings.Contains(f.Template, ".Access") {
			return true
		}
	}
	for _, a := range m.Launch.Args {
		if strings.Contains(a, ".Access") {
			return true
		}
	}
	return false
}

// ParseManifest decodes and validates one manifest.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, err
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		return nil, fmt.Errorf("unknown keys: %v", undec)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	var errs []error
	if m.ManifestVersion != ManifestVersion {
		errs = append(errs, fmt.Errorf("manifest_version = %d, want %d", m.ManifestVersion, ManifestVersion))
	}
	if m.Name == "" {
		errs = append(errs, errors.New("name is empty"))
	}
	if m.Launch.Command == "" {
		errs = append(errs, errors.New("launch.command is empty"))
	}
	if m.Launch.Command != "" && strings.ContainsAny(m.Launch.Command, " \t") {
		errs = append(errs, errors.New("launch.command must be one program; put arguments in launch.args"))
	}
	switch m.Inject.Prompt {
	case "", InjectPaste, InjectNone, InjectChannel:
	default:
		errs = append(errs, fmt.Errorf("inject.prompt %q is not paste|channel|none", m.Inject.Prompt))
	}
	if rc := m.RemoteControl; !rc.Supported() && (rc.Enable != "" || rc.Disable != "") {
		errs = append(errs, errors.New("remote_control: enable and disable need args"))
	}
	if d := m.RemoteControl.DisableDialog; d != nil && (m.RemoteControl.Disable == "" || d.Contains == "" || d.Keys == "" || d.Done == "") {
		errs = append(errs, errors.New("remote_control.disable_dialog: needs disable, and contains, keys and done"))
	}
	if sm := m.Inject.SwitchModel; sm != "" && !strings.Contains(sm, "{model}") {
		errs = append(errs, errors.New("inject.switch_model must name {model}"))
	}
	if m.Inject.SwitchModelDefault != "" && m.Inject.SwitchModel == "" {
		errs = append(errs, errors.New("inject.switch_model_default needs switch_model"))
	}
	switch m.Inject.InterruptPaste {
	case "", InterruptPasteBefore, InterruptPasteAfter:
	default:
		errs = append(errs, fmt.Errorf("inject.interrupt_paste %q is not before|after", m.Inject.InterruptPaste))
	}
	if m.Inject.InterruptPaste != "" && m.Inject.Interrupt == "" {
		errs = append(errs, errors.New("inject.interrupt_paste needs interrupt"))
	}
	if l := m.ListModels; l != nil {
		if err := l.validate(); err != nil {
			errs = append(errs, fmt.Errorf("list_models: %w", err))
		}
		if len(m.Launch.ModelArgs) == 0 {
			errs = append(errs, errors.New("list_models needs launch.model_args"))
		}
	}
	for _, e := range m.Guard.Secrets {
		if !validGuardPath(e) {
			errs = append(errs, fmt.Errorf("guard.secrets: %q is not relative to home or under $VAR", e))
		}
	}
	if r := m.Guard.EscapeRule; r != "" && !slices.ContainsFunc(m.Rules, func(x Rule) bool { return x.ID == r && x.State == StateBlocked }) {
		errs = append(errs, fmt.Errorf("guard.escape_rule: %q names no blocked [[rules]] entry", r))
	}
	for _, e := range m.Guard.Writable {
		if !validGuardPath(e) {
			errs = append(errs, fmt.Errorf("guard.writable: %q is not relative to home or under $VAR", e))
		}
	}
	for _, e := range m.Guard.Temp {
		if e == "" || slices.Contains(strings.Split(filepath.ToSlash(e), "/"), "..") || !(strings.HasPrefix(e, "/") || strings.HasPrefix(e, "$")) {
			errs = append(errs, fmt.Errorf("guard.temp: %q is not absolute or under $VAR", e))
		}
	}
	for _, e := range m.Guard.TempPrivate {
		if !filepath.IsLocal(e) {
			errs = append(errs, fmt.Errorf("guard.temp_private: %q is not a folder inside the temp folder", e))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(m.Guard.Tools)) {
		t := m.Guard.Tools[name]
		if !slices.Contains(guard.Kinds, t.Kind) {
			errs = append(errs, fmt.Errorf("guard.tools.%s: kind %q is not shell|write|patch|read|glob", name, t.Kind))
		}
		if len(t.Fields) == 0 || slices.Contains(t.Fields, "") {
			errs = append(errs, fmt.Errorf("guard.tools.%s: fields must name the input fields the guard reads", name))
		}
		if t.Syntax != "" && (t.Kind != guard.KindShell || !slices.Contains(guard.Syntaxes, t.Syntax)) {
			errs = append(errs, fmt.Errorf("guard.tools.%s: syntax %q is not powershell on a shell tool", name, t.Syntax))
		}
	}
	switch m.Screen.Resize {
	case "", ResizeFollow, ResizeExplicit:
	default:
		errs = append(errs, fmt.Errorf("screen.resize %q is not follow|explicit", m.Screen.Resize))
	}
	valid := map[State]bool{"": true, StateIdle: true, StateWorking: true, StateBlocked: true, StateExited: true}
	for i, h := range m.Hooks {
		if h.Event == "" {
			errs = append(errs, fmt.Errorf("hooks[%d]: event is empty", i))
		}
		if !valid[h.State] {
			errs = append(errs, fmt.Errorf("hooks[%d]: unknown state %q", i, h.State))
		}
		if h.Command != "" && (h.Command != CommandSaveClear && h.Command != CommandAnswer || h.Respond == "") {
			errs = append(errs, fmt.Errorf("hooks[%d]: command %q is not saveclear|answer, with a respond", i, h.Command))
		}
		if h.Counter != "" && (len(h.Counter) < 2 || (h.Counter[0] != '+' && h.Counter[0] != '-') || h.CounterKey == "") {
			errs = append(errs, fmt.Errorf("hooks[%d]: counter must be +name or -name, with counter_key", i))
		}
		switch h.Timeout {
		case "", HookTimeoutResponse, HookTimeoutContext, HookTimeoutCommand, HookTimeoutGuard:
			if h.Timeout != "" && h.Respond == "" {
				errs = append(errs, fmt.Errorf("hooks[%d]: timeout needs respond", i))
			}
		default:
			errs = append(errs, fmt.Errorf("hooks[%d]: timeout %q is not response|context|command|guard", i, h.Timeout))
		}
	}
	for i, t := range m.Todos {
		if err := t.validate(); err != nil {
			errs = append(errs, fmt.Errorf("todos[%d]: %w", i, err))
		}
	}
	if f := m.StatusFile; f != nil {
		if f.Path == "" || f.StateField == "" || len(f.StateMap) == 0 {
			errs = append(errs, errors.New("status_file: needs path, state_field and state_map"))
		}
		for k, v := range f.StateMap {
			if !valid[v] || v == "" {
				errs = append(errs, fmt.Errorf("status_file.state_map %q -> unknown state %q", k, v))
			}
		}
	}
	if t := m.JSONLTail; t != nil {
		if t.PathField == "" || (len(t.Rules) == 0 && t.Usage == nil && len(t.Refused) == 0) {
			errs = append(errs, errors.New("jsonl_tail: needs path_field, and rules, usage or refused"))
		}
		for i, r := range t.Refused {
			if len(r.Match) == 0 {
				errs = append(errs, fmt.Errorf("jsonl_tail.refused[%d]: needs match", i))
			}
		}
		if t.Usage != nil {
			if err := t.Usage.validate(); err != nil {
				errs = append(errs, fmt.Errorf("jsonl_tail.usage: %w", err))
			}
		}
		for i, r := range t.Rules {
			if !valid[r.State] || r.State == "" {
				errs = append(errs, fmt.Errorf("jsonl_tail.rules[%d]: unknown state %q", i, r.State))
			}
		}
	}
	if t := m.TodoSnapshot; t != nil && (t.Dir == "" || t.Glob == "" || t.ID == "" || t.Text == "" || t.Status == "") {
		errs = append(errs, errors.New("todos_snapshot: needs dir, glob, id, text and status"))
	}
	for i, r := range m.Rules {
		// A rule may also say "unknown": a screen on which no state can be
		// read (e.g. a transcript view), so the screen abstains.
		if r.ID == "" || r.State == "" || !(valid[r.State] || r.State == StateUnknown) {
			errs = append(errs, fmt.Errorf("rules[%d]: needs an id and a valid state", i))
		}
		if r.Keys != "" && r.State != StateBlocked {
			errs = append(errs, fmt.Errorf("rules[%d] %s: keys answer a dialog: the rule must be blocked", i, r.ID))
		}
		if r.Regex != "" {
			if _, err := regexp.Compile(r.Regex); err != nil {
				errs = append(errs, fmt.Errorf("rules[%d] %s: %w", i, r.ID, err))
			}
		}
	}
	if a := m.Answer; a.Rule != "" || a.TextOption != "" || a.Submit != "" || a.TextKey != "" || a.TextFocus != "" {
		found := false
		for _, x := range m.Rules {
			found = found || (x.ID == a.Rule && x.State == StateBlocked)
		}
		if !found {
			errs = append(errs, fmt.Errorf("answer.rule %q names no blocked rule", a.Rule))
		}
		if (a.TextOption == "") != (a.Submit == "") {
			errs = append(errs, errors.New("answer: text_option and submit go together"))
		}
		if (a.TextKey != "" || a.TextFocus != "") && a.TextOption == "" {
			errs = append(errs, errors.New("answer: text_key and text_focus need text_option"))
		}
	}
	if r := m.Inject.EmptyRule; r != "" {
		found := false
		for _, x := range m.Rules {
			found = found || x.ID == r
		}
		if !found {
			errs = append(errs, fmt.Errorf("inject.empty_rule %q names no rule", r))
		}
	}
	for _, f := range m.Launch.Files {
		if filepath.IsAbs(f.Path) || strings.Contains(f.Path, "..") {
			errs = append(errs, fmt.Errorf("launch.files %q must be relative, inside the runtime dir", f.Path))
		}
	}
	if f := m.Theme; f.Path != "" || f.Template != "" {
		if f.Path == "" || f.Template == "" {
			errs = append(errs, errors.New("theme: path and template go together"))
		} else if filepath.IsAbs(f.Path) || strings.Contains(f.Path, "..") {
			errs = append(errs, fmt.Errorf("theme.path %q must be relative, inside the agent's config folder", f.Path))
		}
	}
	return errors.Join(errs...)
}

// Resize is when a pane running the agent is resized (screen.resize).
type Resize string

const (
	ResizeFollow   Resize = "follow"   // also to the console typed in
	ResizeExplicit Resize = "explicit" // only on a window resize or split change
)

// FollowsTyping reports whether typing into a console may resize a pane
// running a (nil: no agent, a shell).
func FollowsTyping(a Agent) bool {
	m := ManifestOf(a)
	return m == nil || m.Screen.Resize != ResizeExplicit
}

// manifestAgent implements Agent from a Manifest alone.
type manifestAgent struct{ m *Manifest }

// ManifestOf returns the manifest an agent was built from, or nil for an
// agent that has none.
func ManifestOf(a Agent) *Manifest {
	if x, ok := a.(interface{ Manifest() *Manifest }); ok {
		return x.Manifest()
	}
	return nil
}

func (a *manifestAgent) Manifest() *Manifest { return a.m }

// FromManifest returns an Agent driven entirely by m.
func FromManifest(m *Manifest) Agent { return &manifestAgent{m: m} }

func (a *manifestAgent) Name() string { return a.m.Name }

func (a *manifestAgent) Identify(p ProcessInfo) bool {
	if len(p.Argv) == 0 {
		return false
	}
	base := filepath.Base(p.Argv[0])
	for _, n := range a.m.Identify.Argv0 {
		if base == n {
			return true
		}
	}
	return false
}

// launchData is what launch templates see: the LaunchSpec's fields and
// the manifest's .HookEvents.
type launchData struct {
	LaunchSpec
	HookEvents []string
}

// HookEvents are the hook events the manifest names, in manifest order,
// each once: those of [[hooks]], [[todos]] and todos_snapshot.on. A
// harness that registers hooks per event registers these, and an event
// none of them names isn't worth a `tm hook` process.
func (m *Manifest) HookEvents() []string {
	var out []string
	seen := map[string]bool{}
	add := func(ev string) {
		if ev != "" && !seen[ev] {
			seen[ev] = true
			out = append(out, ev)
		}
	}
	for _, h := range m.Hooks {
		add(h.Event)
	}
	for _, t := range m.Todos {
		add(t.Event)
	}
	if m.TodoSnapshot != nil {
		for _, ev := range m.TodoSnapshot.On {
			add(ev)
		}
	}
	return out
}

func (a *manifestAgent) Launch(spec LaunchSpec) (Launch, error) {
	l := a.m.Launch
	var out Launch
	if spec.RemoteControl && !a.m.RemoteControl.Supported() {
		return out, fmt.Errorf("agent %s has no remote control", a.m.Name)
	}
	if spec.Resume && spec.AgentSID == "" {
		// Some CLIs open an interactive picker for an empty id.
		return out, fmt.Errorf("agent %s: resume needs the agent's session id", a.m.Name)
	}
	if spec.Role != "thread" {
		// Sandbox lists are threads' only: whatever asks, a coordinator's
		// settings never get them.
		spec.Sandbox = SandboxLists{}
	}
	data := launchData{spec, a.m.HookEvents()}
	argv := []string{l.Command}
	add := func(tmpls []string) error {
		for _, t := range tmpls {
			s, err := render(t, data)
			if err != nil {
				return err
			}
			if s != "" {
				argv = append(argv, s)
			}
		}
		return nil
	}
	steps := []struct {
		on    bool
		tmpls []string
	}{
		{true, l.Args},
		{spec.Resume, l.ResumeArgs},
		{spec.Yolo, l.YoloArgs},
		{spec.Model != "", l.ModelArgs},
		{spec.RemoteControl, a.m.RemoteControl.Args},
		{spec.Kickoff != "" && !spec.Resume, l.KickoffArgs},
	}
	for _, s := range steps {
		if s.on {
			if err := add(s.tmpls); err != nil {
				return out, err
			}
		}
	}
	out.Argv = argv
	out.Kickoff = spec.Kickoff != "" && !spec.Resume && len(l.KickoffArgs) > 0
	out.Unset = append([]string{}, l.UnsetEnv...)
	for k, v := range l.Env {
		s, err := render(v, data)
		if err != nil {
			return out, err
		}
		if s == "" {
			continue // nothing to set this launch, e.g. no temp folder
		}
		out.Env = append(out.Env, k+"="+s)
	}
	out.Files = map[string][]byte{}
	for _, f := range l.Files {
		s, err := render(f.Template, data)
		if err != nil {
			return out, fmt.Errorf("launch.files %s: %w", f.Path, err)
		}
		if strings.TrimSpace(s) == "" {
			continue // nothing to say this launch, e.g. no theme
		}
		out.Files[f.Path] = []byte(s)
	}
	return out, nil
}

func (a *manifestAgent) Hook(ev HookEvent, env HookEnv) ([]Signal, HookResult, error) {
	ignored := false
	for _, f := range a.m.IgnoreFields {
		if s, ok := lookupString(ev.Payload, f); ok && s != "" {
			ignored = true
			break
		}
	}
	var sigs []Signal
	var res HookResult
	base := Signal{Source: "hook", Seq: ev.Seq, At: ev.At}
	if !ignored && a.m.SessionField != "" {
		if sid, _ := lookupString(ev.Payload, a.m.SessionField); sid != "" {
			sig := base
			sig.AgentSID = sid
			sigs = append(sigs, sig)
		}
	}
	for _, h := range a.m.Hooks {
		if h.Event != ev.Event || !matches(h.Match, ev.Payload) {
			continue
		}
		if h.Counter != "" {
			sig := base
			sig.Counter = h.Counter
			sig.CounterKey, _ = lookupString(ev.Payload, h.CounterKey)
			sigs = append(sigs, sig)
		}
		// The first entry whose response says something answers; one
		// that renders nothing (a guard that let the call through, a
		// command that didn't run) leaves it to the next.
		answered := false
		if h.Respond != "" && len(res.Stdout) == 0 {
			out, ran, err := renderRespond(h.Respond, h.Command, ev, env, a.m.Hook)
			if err != nil {
				return sigs, res, err
			}
			res.Stdout, answered = out, ran
		}
		// A command's entry sets its state only when the command ran
		// and said what came of it: otherwise the prompt goes on, and
		// the event's other entries say the state. An ignored event
		// (ignore_fields: a subagent's) sets none, but is answered all
		// the same: its tool call is judged by the guard like any other.
		if !ignored && h.State != "" && (h.Command == "" || answered) {
			sig := base
			sig.State, sig.Reason, sig.Transient = h.State, h.Reason, h.Transient
			sigs = append(sigs, sig)
		}
	}
	if ignored {
		return sigs, res, nil
	}
	for _, tm := range a.m.Todos {
		if tm.Event != ev.Event || !matches(tm.Match, ev.Payload) {
			continue
		}
		ch, err := tm.change(ev.Payload)
		if err != nil {
			return sigs, res, fmt.Errorf("agent %s: todos on %s: %w", a.m.Name, ev.Event, err)
		}
		sig := base
		sig.Todo = &ch
		sigs = append(sigs, sig)
		break
	}
	return sigs, res, nil
}

// GuardEvent reports whether agent a's manifest has the guard answer
// event (an entry with timeout = "guard").
func GuardEvent(a Agent, event string) bool {
	m := ManifestOf(a)
	if m == nil {
		return false
	}
	return slices.ContainsFunc(m.Hooks, func(h HookMap) bool { return h.Event == event && h.Timeout == HookTimeoutGuard })
}

// FailClosed is what agent a's hook answers to event when the guard's
// verdict didn't come (no server, a timeout, a refusal, a payload tm
// hook couldn't read): its own response with guard.Unchecked as the
// verdict, so the call is refused, never let through unjudged; nil for
// an event the guard doesn't answer. payload may be nil.
func FailClosed(a Agent, event string, payload map[string]any) []byte {
	if !GuardEvent(a, event) {
		return nil
	}
	m := ManifestOf(a)
	p := maps.Clone(payload)
	if p == nil {
		p = map[string]any{}
	}
	// A payload tm hook couldn't read names no tool: the verdict is
	// asked for all the same.
	if t, _ := p[m.Hook.ToolField()].(string); t == "" {
		p[m.Hook.ToolField()] = "unknown"
	}
	_, res, err := a.Hook(HookEvent{Agent: a.Name(), Event: event, Payload: p},
		HookEnv{Guard: func(string, map[string]any) *guard.Denial { return guard.Unchecked() }})
	if err != nil {
		return nil
	}
	return res.Stdout
}

func (a *manifestAgent) Sources() *Sources { return &a.m.Sources }

func (a *manifestAgent) Rules() []Rule { return a.m.Rules }

func (a *manifestAgent) Injector() Injector {
	if a.m.Inject.Prompt == "" {
		return InjectPaste
	}
	return a.m.Inject.Prompt
}

func (a *manifestAgent) Prompt(context.Context, PromptTarget, string) error {
	return fmt.Errorf("agent %s: no structured prompt channel; inject.prompt = %q", a.m.Name, a.Injector())
}

var funcs = template.FuncMap{
	// json encodes a value as a JSON string literal or object.
	"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	},
	// hookexec is the exec form of a command hook, no shell on any OS:
	// the "command" and "args" members of the hook object, e.g.
	// hookexec .TMBin "hook" "--agent" "claude" ->
	// "command":"/bin/tm","args":["hook","--agent","claude"].
	"hookexec": func(command string, args ...string) (string, error) {
		if args == nil {
			args = []string{}
		}
		c, err := json.Marshal(command)
		if err != nil {
			return "", err
		}
		a, err := json.Marshal(args)
		return fmt.Sprintf(`"command":%s,"args":%s`, c, a), err
	},
	// rules formats one permission rule per directory, e.g.
	// rules "Read(/%s/**)" .Access.Read -> ["Read(//home/u/p/**)"].
	// A Read or Edit rule is a glob, so the path's glob characters are
	// escaped to match literally; other rules (Bash) take the text as is.
	"rules": func(format string, dirs []string) []string {
		glob := strings.HasPrefix(format, "Read(") || strings.HasPrefix(format, "Edit(")
		out := make([]string, 0, len(dirs))
		for _, d := range dirs {
			d = posixPath(d)
			if glob {
				d = escapeGlob(d)
			}
			out = append(out, fmt.Sprintf(format, d))
		}
		return out
	},
	// siblingRules formats deny rules for the siblings of each folder
	// in dirs (Access.NoWriteSiblings) as name patterns (siblingGlobs),
	// e.g. siblingRules "Edit(/%s)" .Access.NoWriteSiblings.
	"siblingRules": func(format string, dirs []string) []string {
		var out []string
		for _, d := range dirs {
			for _, g := range siblingGlobs(d) {
				out = append(out, fmt.Sprintf(format, g))
			}
		}
		return out
	},
	// file is a file's text, e.g. the brief for an agent that takes it
	// as a value rather than a path: toml (file .BriefPath).
	"file": readText,
	// toml quotes a string as a TOML basic string, for a CLI that parses
	// a -c key=value as TOML (Codex's developer_instructions).
	"toml": TOMLString,
	// pathmodes is the file grants of an access policy as a TOML inline
	// table of path = "read" | "write" | "none", for a sandbox that takes one
	// mode per path (Codex's permission profile filesystem); any more
	// arguments are directories to write besides (a coordinator's cwd).
	"pathmodes": pathModes,
	// blend is colour a moved pct percent of the way to colour b, both
	// #rrggbb, e.g. a background a shade lighter; "" when either is.
	"blend": blend,
	// colors is a JSON object of name, colour pairs, leaving out those
	// with no colour: colors "error" .Theme.Fg.bad "success" "" ->
	// {"error":"#bf616a"}.
	"colors": colors,
	// list is its strings when cond holds, else none, for a grant only
	// some launches get: list (not .MCPClash) "mcp__terminatr".
	"list": func(cond bool, s ...string) []string {
		if !cond {
			return []string{}
		}
		return s
	},
	// concat joins lists, for building one JSON array from several grants.
	"concat": func(lists ...[]string) []string {
		out := []string{}
		for _, l := range lists {
			out = append(out, l...)
		}
		return out
	},
}

func blend(a, b string, pct int) string {
	x, errA := theme.ParseRGB(a)
	y, errB := theme.ParseRGB(b)
	if errA != nil || errB != nil || x == nil || y == nil {
		return ""
	}
	mix := func(p, q uint8) int { return (int(p)*(100-pct) + int(q)*pct + 50) / 100 }
	return fmt.Sprintf("#%02x%02x%02x", mix(x.R, y.R), mix(x.G, y.G), mix(x.B, y.B))
}

func colors(pairs ...string) (string, error) {
	if len(pairs)%2 != 0 {
		return "", fmt.Errorf("colors takes name, colour pairs, got %d values", len(pairs))
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			continue
		}
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(pairs[i])
		v, _ := json.Marshal(pairs[i+1])
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.String(), nil
}

// escapeGlob puts a backslash before each glob metacharacter in p, so a
// permission rule built from the path matches only that path.
func escapeGlob(p string) string {
	if !strings.ContainsAny(p, "*?[]{}!") {
		return p
	}
	var b strings.Builder
	for _, r := range p {
		if strings.ContainsRune("*?[]{}!", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// nameChars are the characters siblingGlobs tells names apart by:
// those of tm's worktree names (t-0001-title) and most of git's.
const nameChars = "-0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz"

// siblingGlobs are glob patterns for every folder beside d but d,
// those made later too. A glob has no "but": a name is matched where it
// first differs from d's, by each prefix of d's name followed by any
// other of nameChars, and by d's name followed by more; each shorter
// prefix is a pattern by itself. A name that first differs from d's by
// a character outside nameChars, or only by a letter's case, isn't
// matched. None when d's path holds a character a glob would read, nor
// for a Windows path (its backslashes), where Claude reads no brackets.
func siblingGlobs(d string) []string {
	parent, name := filepath.Dir(d), filepath.Base(d)
	if !GlobSafe(d) || strings.Contains(d, `\`) || name == "" || parent == d {
		return nil
	}
	out := make([]string, 0, 2*len(name)+1)
	for i := 0; i < len(name); i++ {
		out = append(out, parent+"/"+name[:i]+charClass(name[i])+"*")
		if i > 0 {
			out = append(out, parent+"/"+name[:i])
		}
	}
	return append(out, parent+"/"+name+charClass(0)+"*")
}

// GlobSafe reports whether path p holds no character a permission
// rule's glob reads, so that a pattern built on it (siblingGlobs) names
// it as it is: Claude's sandbox takes no escapes there.
func GlobSafe(p string) bool { return !strings.ContainsAny(p, "*?[]{}!") }

// charClass is a bracket expression of nameChars but c, in ranges.
// Claude's sandbox on macOS matches paths without regard to case, so a
// letter c is left out in both cases.
func charClass(c byte) string {
	var b strings.Builder
	b.WriteByte('[')
	cs := strings.Map(func(r rune) rune {
		if c != 0 && unicode.ToLower(r) == unicode.ToLower(rune(c)) {
			return -1
		}
		return r
	}, nameChars)
	for i := 0; i < len(cs); {
		j := i
		for j+1 < len(cs) && cs[j+1] == cs[j]+1 {
			j++
		}
		switch {
		case j-i >= 2:
			b.WriteString(cs[i:i+1] + "-" + cs[j:j+1])
		default:
			b.WriteString(cs[i : j+1])
		}
		i = j + 1
	}
	b.WriteByte(']')
	return b.String()
}

// posixPath is d in the form Claude matches permission rules against on
// Windows: C:\Users\a becomes /c/Users/a. Any other path is unchanged.
func posixPath(d string) string {
	if len(d) >= 2 && d[1] == ':' && (d[0] >= 'a' && d[0] <= 'z' || d[0] >= 'A' && d[0] <= 'Z') && (len(d) == 2 || d[2] == '\\' || d[2] == '/') {
		return "/" + strings.ToLower(d[:1]) + strings.ReplaceAll(d[2:], "\\", "/")
	}
	return d
}

// pathModes renders a's paths as {"/p"="read","/q"="write",…}: Read
// is read, Write, WriteFiles and the write dirs (by real path, as the
// policy names them) are write, NoWrite and NoWriteFiles are read and win over a
// write of the same path, NoRead is none (no access), winning over
// both, and OwnTemp, inside a NoRead folder, is write. Each path appears
// once, in policy order.
func pathModes(a Access, write ...string) string {
	var order []string
	mode := map[string]string{}
	set := func(paths []string, m string, wins bool) {
		for _, p := range paths {
			old, seen := mode[p]
			if !seen {
				order = append(order, p)
			}
			if !seen || wins || old == "read" && m == "write" {
				mode[p] = m
			}
		}
	}
	set(a.Read, "read", false)
	set(a.Write, "write", false)
	set(a.WriteFiles, "write", false)
	for _, d := range write {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			d = r
		}
		set([]string{d}, "write", false)
	}
	set(a.NoWrite, "read", true)
	set(a.NoWriteFiles, "read", true)
	set(a.NoRead, "none", true)
	if a.OwnTemp != "" {
		set([]string{a.OwnTemp}, "write", true)
	}
	parts := make([]string, len(order))
	for i, p := range order {
		parts[i] = TOMLString(p) + "=" + TOMLString(mode[p])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// maxFileText bounds what the file template func reads: it ends up in
// an argv or a generated file.
const maxFileText = 256 << 10

func readText(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file %q: not an absolute path", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFileText+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxFileText {
		return "", fmt.Errorf("file %s: larger than %d bytes", path, maxFileText)
	}
	return string(b), nil
}

// TOMLString is s as a TOML basic string: quotes, backslashes and
// control characters escaped, invalid UTF-8 replaced.
func TOMLString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// RenderTheme renders [theme]'s template with data (the LiveThemer's
// own), with the launch templates' functions.
func (m *Manifest) RenderTheme(data any) (string, error) {
	s, err := render(m.Theme.Template, data)
	if err != nil {
		return "", fmt.Errorf("theme %s: %w", m.Theme.Path, err)
	}
	return s, nil
}

func render(tmpl string, data any) (string, error) {
	t, err := template.New("").Funcs(funcs).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// renderRespond renders a hook response. .Context and .Guard are
// methods so the (possibly expensive) context is rendered, and the call
// judged, only if the template uses them. ran says the entry's command
// ran and said what came of it.
func renderRespond(tmpl, command string, ev HookEvent, env HookEnv, hook HookTrim) (out []byte, ran bool, err error) {
	d := &respondData{ev: ev, env: env, hook: hook, command: command}
	s, err := render(tmpl, d)
	return []byte(s), d.result != "", err
}

type respondData struct {
	command string
	ran     bool
	result  string
	ev      HookEvent
	env     HookEnv
	hook    HookTrim
	judged  bool
	denial  *guard.Denial
}

// Command runs the entry's session command (HookMap.Command) once and
// is what came of it, "" when it didn't run.
func (d *respondData) Command() string {
	if d.ran || d.command == "" || d.env.Command == nil {
		return d.result
	}
	d.ran = true
	d.result = d.env.Command(d.command)
	return d.result
}

func (d *respondData) Event() string           { return d.ev.Event }
func (d *respondData) Payload() map[string]any { return d.ev.Payload }
func (d *respondData) Context() (string, error) {
	if d.env.Context == nil {
		return "", nil
	}
	b, err := d.env.Context()
	return string(b), err
}

// Guard is the guard's refusal of the tool call in the payload (the
// [hook] tool_field and input_field), or nil: a template answers with it, e.g.
// {{with .Guard}}…{{json .Message}}…{{end}}. The call is judged (and a
// refusal recorded) once however often the template asks.
func (d *respondData) Guard() *guard.Denial {
	if d.judged || d.env.Guard == nil {
		return d.denial
	}
	d.judged = true
	tool, _ := d.ev.Payload[d.hook.ToolField()].(string)
	input, _ := d.ev.Payload[d.hook.InputField()].(map[string]any)
	if tool == "" {
		// A call that names no tool can't be judged: refused.
		d.denial = guard.Unchecked()
		return d.denial
	}
	d.denial = d.env.Guard(tool, input)
	return d.denial
}
