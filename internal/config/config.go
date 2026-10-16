// Package config reads and writes ~/.terminatr/config.toml, the human's
// settings (docs/SPEC.md §5.1, §11.2): the per-project safety settings
// under [projects.<slug>], the all-projects ones under [defaults] that a
// project follows for every key it doesn't set (among them the agents
// threads and coordinators run), and the TUI's prefix key ([keys] prefix) and icon set ([ui] icons), whose
// values the TUI checks, and whether sessions load terminatr's mod
// ([agent_mod] enabled, docs/SPEC.md §8.6).
//
// Changing safety settings is a human action: tm writes this file only
// from the TUI's settings popups, on the human's keypress (write.go). It
// lives outside every project folder, so a coordinator editing
// PROJECT.md can't touch it, and coordinators get an Edit deny rule on it.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/theclifmeister/terminatr/internal/home"
)

// Values of start_threads.
const (
	StartPropose = "propose" // threads start only after the human's go-ahead
	StartAuto    = "auto"
)

// Values of auto_close: when the ticker closes (resolves) a finished
// thread (§9).
const (
	CloseOff    = "off"
	CloseMerged = "merged" // once its PR merged
	CloseDays   = "days"   // auto_close_days after it finished
)

// Values of complete_tasks: when a task is done (§6.4, §7.5).
// CompleteUser leaves it to the user; CompleteMerged is the user's
// standing acceptance, applied by the ticker to a task in review once
// its thread's pull request merged.
const (
	CompleteUser   = "user"
	CompleteMerged = "merged" // its PR merged
	// CompleteRemoved is the former "when released" (a tag contains the
	// PR's merge commit), dropped as too specific to one release flow.
	// It is read as CompleteUser, so nothing completes silently, and tm
	// doctor warns until the user picks again (Removed).
	CompleteRemoved = "released"
)

// Values of merge: who merges a thread's pull request. The mod's guard
// (docs/SPEC.md §8.6, Guard) refuses a thread's `gh pr merge` (or the Azure DevOps equivalent) under
// MergeCoordinator.
const (
	MergeCoordinator = "coordinator"
	MergeThread      = "thread"
)

// GuardRules are the ids of the mod's guard rules (docs/SPEC.md §8.6,
// Guard), which guard_off may name.
var GuardRules = []string{"force-push", "push-default", "worktree-only", "delete-branch", "merge", "credentials"}

// DefaultContextHint is [ui] context_hint when unset: the percent of
// its model's context window at which a coordinator is told to consider
// /clear.
const DefaultContextHint = 40

// Limits of the number settings.
const (
	MaxParallelThreads = 99
	// MaxGlobalThreads bounds global_parallel_threads; 0 is no cap.
	MaxGlobalThreads = 999
	MaxAutoCloseDays = 365
	MaxArchiveDays   = 365
	// MinPRPollSeconds and MaxPRPollSeconds bound pr_poll_seconds.
	MinPRPollSeconds     = 30
	MaxPRPollSeconds     = 3600
	DefaultPRPollSeconds = 120
)

// ArchiveKeys are the retention settings (docs/SPEC.md §7.6): how many
// days the ticker keeps done tasks on the board, resolved threads'
// folders, handled inbox items as loose files, and lines in JOURNAL.md
// before it moves them to their archives.
var ArchiveKeys = []string{"archive_tasks_days", "archive_threads_days", "archive_inbox_days", "archive_journal_days"}

// Safety is one project's resolved safety settings.
type Safety struct {
	StartThreads        string `json:"start_threads"`
	Yolo                bool   `json:"yolo"`
	CoordinatorApproves bool   `json:"coordinator_approves"`
	// ParallelThreads is the most threads that may work at once: tm
	// thread start refuses beyond it without --over-cap (§9).
	ParallelThreads int `json:"parallel_threads"`
	// GlobalParallelThreads is the most threads that may work at once
	// across every project, 0 for no such cap: tm thread start refuses
	// beyond it too (§9). Only [defaults] sets it (DefaultsOnly).
	GlobalParallelThreads int `json:"global_parallel_threads"`
	// AutoClose is when the ticker resolves a finished thread whose agent
	// rests (§9): CloseOff, CloseMerged, or CloseDays after
	// AutoCloseDays. Resolve's usual rules hold (never forced), and a
	// thread with uncommitted or unpushed work is never closed.
	AutoClose     string `json:"auto_close"`
	AutoCloseDays int    `json:"auto_close_days"`
	// PRFollowup prompts a thread when its PR's checks fail or a reviewer
	// asks for changes (§7.5).
	PRFollowup bool `json:"pr_followup"`
	// PRPollSeconds is how often the ticker asks the PR host about each open
	// thread's PR, and fetches the repos (§7.5).
	PRPollSeconds int `json:"pr_poll_seconds"`
	// CompleteTasks is when a task is done: CompleteUser (only on the
	// user's word) or CompleteMerged (the ticker marks a task in review
	// done once its thread's PR merged).
	CompleteTasks string `json:"complete_tasks"`
	// CoordinatorRemoteControl starts the coordinator with the agent's
	// remote control on (e.g. Claude Code's Remote Control), named after
	// the project (§8.2 [remote_control]). The prefix key and tm project
	// remote change only the running session.
	CoordinatorRemoteControl bool `json:"coordinator_remote_control"`
	// AutoClear lets the ticker clear the coordinator's conversation once
	// its context reaches [ui] context_hint, only while nothing waits on
	// it (§7.5): the project's state lives in files, so nothing is lost.
	AutoClear bool `json:"auto_clear"`
	// CoordinatorMerges adds a permission allow rule for tm thread merge
	// (which makes the PR merge itself, on GitHub and Azure DevOps) to the
	// coordinator's launch settings only, so an auto-mode classifier that
	// doesn't see the standing merge rule stops refusing it. Threads never
	// get it, and the guard still refuses their merges. Only effective
	// with Merge = MergeCoordinator.
	CoordinatorMerges bool `json:"coordinator_merges"`
	// FastForwardCheckout lets the ticker fast-forward the user's own
	// checkout of a project repo to origin's default branch when that
	// branch is checked out, clean and only behind (§7.5).
	FastForwardCheckout bool `json:"fast_forward_checkout"`
	// SandboxEscapes lets a thread's agent ask to run a command outside
	// its sandbox, which the human or auto mode then approves (§5.2).
	// Off, Claude threads start with sandbox.allowUnsandboxedCommands =
	// false: what can't run in the sandbox doesn't run.
	SandboxEscapes bool `json:"sandbox_escapes"`
	// ThreadAgent is the agent tm thread start runs without --agent, and
	// CoordinatorAgent the one a new coordinator runs (§8, §11.2): names
	// from the agent registry (tm agent list). "" when no table sets
	// them: internal/models resolves that to the one installed agent, or
	// asks the user when there are several. The registry is checked where
	// they are used, since config doesn't read the manifests.
	ThreadAgent      string `json:"thread_agent"`
	CoordinatorAgent string `json:"coordinator_agent"`
	// Models is the project's scope: the models the coordinator may pick
	// for a thread (tm thread start --model, §8.2, §11.2), names from
	// what the installed agents list. nil is every available model.
	Models []string `json:"models,omitempty"`
	// CoordinatorModel is the model a new coordinator runs, from the
	// project's scope and its coordinator agent's models; "" is the
	// agent's default.
	CoordinatorModel string `json:"coordinator_model,omitempty"`
	// Active: the project's coordinator and threads run and are resumed
	// at a server start; an inactive project's stay dormant, kept to
	// resume at its activation, and the ticker leaves it alone (§5.1).
	Active bool `json:"active"`
	// Paused stops the ticker's prompts (nudges, PR follow-up) and new
	// threads of the project; state polling goes on (§7.5, §11.2).
	Paused bool `json:"paused"`
	// Archived hides the project from the sidebar and
	// stops all ticker work for it (§5.1).
	Archived bool `json:"archived"`
	// Pinned puts the project at the top of the sidebar's active
	// projects; it changes their order only (§4).
	Pinned bool `json:"pinned"`
	// Share offers the project to the paired servers (docs/SPEC.md
	// §16.9): its state, read-only, never its files. Off until the user
	// turns it on; a project's own, never all projects'.
	Share bool `json:"share"`
	// Merge is who merges a thread's PR: MergeCoordinator or MergeThread.
	Merge string `json:"merge"`
	// BranchName is the template of new threads' branches
	// (ExpandBranchName); existing threads keep the branch they have.
	BranchName string `json:"branch_name"`
	// Guard turns the mod's guard on (docs/SPEC.md §8.6, Guard): it
	// refuses tool calls that break the standing rules. GuardOff are the
	// GuardRules it leaves out. Only the human sets them, here.
	Guard    bool     `json:"guard"`
	GuardOff []string `json:"guard_off,omitempty"`
	// The retention ages in days (ArchiveKeys): after them the ticker
	// moves done tasks to the task archive, packs resolved threads'
	// folders, and bundles handled inbox items and journal lines into
	// compressed monthly files.
	ArchiveTasksDays   int `json:"archive_tasks_days"`
	ArchiveThreadsDays int `json:"archive_threads_days"`
	ArchiveInboxDays   int `json:"archive_inbox_days"`
	ArchiveJournalDays int `json:"archive_journal_days"`
}

// ArchiveDays is the retention setting key names (ArchiveKeys), 0 for
// another key.
func (s Safety) ArchiveDays(key string) int {
	if !slices.Contains(ArchiveKeys, key) {
		return 0
	}
	return s.Value(key).(int)
}

// LegacyAgent is what a session or thread record without an agent ran:
// records from before tm had more than one. Never a default: unset
// agents are resolved against the installed ones (internal/models).
const LegacyAgent = "claude"

// Defaults are the settings of a project that neither its own table nor
// [defaults] (all projects) name.
var Defaults = Safety{StartThreads: StartPropose, Yolo: false, CoordinatorApproves: true,
	ParallelThreads: 10, AutoClose: CloseMerged, AutoCloseDays: 7, PRFollowup: true, PRPollSeconds: DefaultPRPollSeconds,
	CompleteTasks: CompleteUser, FastForwardCheckout: true, SandboxEscapes: true, Merge: MergeCoordinator, BranchName: DefaultBranchName, Guard: true, Active: true,
	ArchiveTasksDays: 30, ArchiveThreadsDays: 30, ArchiveInboxDays: 30, ArchiveJournalDays: 30}

type rawSafety struct {
	StartThreads        *string `toml:"start_threads"`
	Yolo                *bool   `toml:"yolo"`
	CoordinatorApproves *bool   `toml:"coordinator_approves"`
	ParallelThreads     *int    `toml:"parallel_threads"`
	GlobalThreads       *int    `toml:"global_parallel_threads"`
	AutoClose           *string `toml:"auto_close"`
	AutoCloseDays       *int    `toml:"auto_close_days"`
	// AutoResolve is auto_close's older form: true is "merged", false
	// "off"; auto_close wins when both are set.
	AutoResolve    *bool     `toml:"auto_resolve"`
	PRFollowup     *bool     `toml:"pr_followup"`
	PRPoll         *int      `toml:"pr_poll_seconds"`
	CompleteTasks  *string   `toml:"complete_tasks"`
	CoordinatorRC  *bool     `toml:"coordinator_remote_control"`
	AutoClear      *bool     `toml:"auto_clear"`
	CoordMerges    *bool     `toml:"coordinator_merges"`
	FastForward    *bool     `toml:"fast_forward_checkout"`
	SandboxEsc     *bool     `toml:"sandbox_escapes"`
	ThreadAgent    *string   `toml:"thread_agent"`
	CoordAgent     *string   `toml:"coordinator_agent"`
	Models         *[]string `toml:"models"`
	CoordModel     *string   `toml:"coordinator_model"`
	Active         *bool     `toml:"active"`
	Paused         *bool     `toml:"paused"`
	Archived       *bool     `toml:"archived"`
	Pinned         *bool     `toml:"pinned"`
	Share          *bool     `toml:"share"`
	Merge          *string   `toml:"merge"`
	BranchName     *string   `toml:"branch_name"`
	Guard          *bool     `toml:"guard"`
	GuardOff       *[]string `toml:"guard_off"`
	ArchiveTasks   *int      `toml:"archive_tasks_days"`
	ArchiveThreads *int      `toml:"archive_threads_days"`
	ArchiveInbox   *int      `toml:"archive_inbox_days"`
	ArchiveJournal *int      `toml:"archive_journal_days"`
	// Repos is the project's repo list, the repositories the user
	// approved for its threads' worktrees (Config.Repos): here, not in
	// PROJECT.md, which the coordinator writes. Not a setting: neither
	// the settings popups nor [defaults] have it.
	Repos *[]string `toml:"repos"`
	// Links are the project's links to other projects' coordinators
	// (links.go): a project's own, never [defaults]'s.
	Links map[string]any `toml:"links"`
}

// CheckAgent checks an agent name's shape: one word. Whether the
// registry has it is checked where it is used.
func CheckAgent(name string) error {
	if name == "" || strings.ContainsAny(name, " \t\n\r\"\\") {
		return fmt.Errorf("must be one agent's name, not %q", name)
	}
	return nil
}

// CheckModels checks a models allow-list's shape: at least one name,
// each one word, none twice. Whether an agent lists them is checked where
// they are used (AllowsModel's callers), since config doesn't read the
// manifests.
func CheckModels(names []string) error {
	if len(names) == 0 {
		return errors.New("must name at least one model (leave it out to allow every model)")
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" || strings.ContainsAny(n, " \t\n\r\"\\") {
			return fmt.Errorf("must be one-word model names, not %q", n)
		}
		if seen[n] {
			return fmt.Errorf("lists %q twice", n)
		}
		seen[n] = true
	}
	return nil
}

// AllowsModel reports whether the allow-list takes model; an empty
// (unset) list takes every model.
func (s Safety) AllowsModel(model string) bool {
	return len(s.Models) == 0 || slices.Contains(s.Models, model)
}

// Config is the parsed file.
type Config struct {
	Path string
	// Prefix is [keys] prefix, or the older [keys] detach that named the
	// same key; Icons is [ui] icons. "" when unset.
	Prefix, Icons string
	// Theme and Layout are [ui] theme and [ui] layout: the id of the
	// enabled mod whose theme, or sidebar layout, every console uses. ""
	// (unset) is the built-in one.
	Theme, Layout string
	// Unknown are the keys under [keys], [ui], [agent_mod] (or [mods]), [mods.<id>] and [agents.<name>] that tm doesn't know
	// (e.g. "ui.icon"), sorted. tm doctor reports them; unlike a
	// project's, they don't fail Load, so a typo there never stops tm.
	Unknown []string
	// AgentMod is [agent_mod] enabled (older [mods] enabled): agent sessions load terminatr's mod, where
	// the agent has one and its version is new enough. Off by default
	// while the mods API is early access.
	AgentMod bool
	// AgentModBand is [agent_mod] band: with the mod loaded, it draws the band
	// above the prompt, its status entry and the CI toast. On unless set
	// false.
	AgentModBand bool
	// AgentModPane is [agent_mod] pane: with the mod loaded, a coordinator session
	// opens its /tm dashboard pane by itself when it starts, where it
	// would dock as a sidebar. Off unless set true (T90: the TUI's info
	// panel shows the same beside the coordinator); /tm opens it anyway.
	AgentModPane bool
	// ContextHint is [ui] context_hint: the percent of its model's
	// context window at which a coordinator is told to consider /clear
	// (DefaultContextHint when unset, 0 for never).
	ContextHint int
	// NeedsFirst is [ui] needs_first: the sidebar lists the projects
	// where something needs you first. Off unless set true.
	NeedsFirst bool
	// Mods are the user mods' [mods.<id>] tables by id (mods.go).
	Mods map[string]Mod
	// UpdateCheck is [ui] update_check: the server looks once a day for a
	// newer release and the dashboard says so. On unless set false.
	UpdateCheck bool
	projects    map[string]rawSafety
	// defaults is the [defaults] table: the all-projects settings.
	defaults rawSafety
	agent    string
	// agents are the [agents.<name>] tables: each agent's models
	// catalog and default (agents.go).
	agents map[string]rawAgent
}

// rawAgentMod is [agent_mod], and the older [mods] it replaced.
type rawAgentMod struct {
	Enabled *bool `toml:"enabled"`
	Band    *bool `toml:"band"`
	Pane    *bool `toml:"pane"`
}

// over returns r with each key it doesn't set taken from old.
func (r rawAgentMod) over(old rawAgentMod) rawAgentMod {
	r.Enabled = cmp.Or(r.Enabled, old.Enabled)
	r.Band = cmp.Or(r.Band, old.Band)
	r.Pane = cmp.Or(r.Pane, old.Pane)
	return r
}

// Path returns <home>/config.toml.
func Path() (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.toml"), nil
}

// Load reads config.toml. A missing file gives the defaults. Unknown keys
// inside a [projects.<slug>] or the [defaults] table are errors, so a
// typo can't silently leave a safety setting at its default; those under
// [keys], [ui] and [agent_mod] are listed in Unknown. With such a safety error, the
// Config is returned too, for its Prefix and Icons: a file that doesn't
// parse gives none.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	var raw struct {
		DefaultAgent string               `toml:"default_agent"`
		Projects     map[string]rawSafety `toml:"projects"`
		Defaults     rawSafety            `toml:"defaults"`
		Agents       map[string]rawAgent  `toml:"agents"`
		Keys         struct {
			Prefix string `toml:"prefix"`
			Detach string `toml:"detach"`
		} `toml:"keys"`
		UI struct {
			Icons       string `toml:"icons"`
			Theme       string `toml:"theme"`
			Layout      string `toml:"layout"`
			ContextHint *int   `toml:"context_hint"`
			UpdateCheck *bool  `toml:"update_check"`
			NeedsFirst  bool   `toml:"needs_first"`
		} `toml:"ui"`
		AgentMod rawAgentMod `toml:"agent_mod"`
		// Mods are the user mods' [mods.<id>] tables and, under the
		// keys enabled, band and pane, the older name of [agent_mod],
		// read for each key [agent_mod] doesn't set.
		Mods map[string]toml.Primitive `toml:"mods"`
	}
	md, err := toml.DecodeFile(path, &raw)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{Path: path, AgentModBand: true, ContextHint: DefaultContextHint, UpdateCheck: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	old, mods, unknownMods, err := readMods(md, raw.Mods)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	am := raw.AgentMod.over(old)
	c := &Config{Mods: mods, Path: path, projects: raw.Projects, defaults: raw.Defaults, agent: raw.DefaultAgent, agents: raw.Agents,
		Prefix: cmp.Or(raw.Keys.Prefix, raw.Keys.Detach), Icons: raw.UI.Icons, Theme: raw.UI.Theme, Layout: raw.UI.Layout, AgentMod: am.Enabled != nil && *am.Enabled,
		AgentModBand: am.Band == nil || *am.Band, AgentModPane: am.Pane != nil && *am.Pane,
		ContextHint: DefaultContextHint, UpdateCheck: raw.UI.UpdateCheck == nil || *raw.UI.UpdateCheck, NeedsFirst: raw.UI.NeedsFirst}
	if h := raw.UI.ContextHint; h != nil {
		if *h < 0 || *h > 100 {
			return c, fmt.Errorf("%s: ui.context_hint is a percent from 0 (never) to 100", path)
		}
		c.ContextHint = *h
	}
	c.Unknown = unknownMods
	for _, k := range md.Undecoded() {
		switch {
		case len(k) >= 4 && k[0] == "projects" && k[2] == "links", len(k) >= 3 && k[0] == "defaults" && k[1] == "links":
			// A link entry's table, read and checked by Links.
		case len(k) >= 3 && k[0] == "projects", len(k) >= 2 && k[0] == "defaults":
			return c, fmt.Errorf("%s: unknown setting %s", path, k.String())
		case len(k) >= 2 && (k[0] == "keys" || k[0] == "ui" || k[0] == "mods" || k[0] == "agent_mod"),
			len(k) >= 3 && k[0] == "agents":
			c.Unknown = append(c.Unknown, k.String())
		}
	}
	sort.Strings(c.Unknown)
	if _, err := c.AllProjects(); err != nil {
		return c, err
	}
	for slug := range raw.Projects {
		if _, err := c.Safety(slug); err != nil {
			return c, err
		}
		if _, err := c.Links(slug); err != nil {
			return c, err
		}
	}
	for _, name := range c.AgentNames() {
		if err := c.agents[name].settings().check(); err != nil {
			return c, fmt.Errorf("%s: agents.%s.%w", path, name, err)
		}
	}
	return c, nil
}

// Safety returns a project's settings: each key the project's own
// value, else the all-projects one ([defaults]), else Defaults.
func (c *Config) Safety(slug string) (Safety, error) {
	s, err := c.AllProjects()
	if err != nil {
		return s, err
	}
	r, ok := c.projects[slug]
	if !ok {
		return s, nil
	}
	// The global cap counts every project's threads: a project's own
	// value would mean nothing.
	if r.GlobalThreads != nil {
		return s, fmt.Errorf("%s: projects.%s can't set global_parallel_threads; it is all projects' cap, in [defaults]", c.Path, slug)
	}
	return s, r.apply(&s, c.Path, "projects."+slug)
}

// Repos is project slug's repo list ([projects.<slug>] repos), the
// repositories the user approved, and whether the table sets it at all.
// A nil Config has none.
func (c *Config) Repos(slug string) (repos []string, set bool) {
	if c == nil {
		return nil, false
	}
	r, ok := c.projects[slug]
	if !ok || r.Repos == nil {
		return nil, false
	}
	return slices.Clone(*r.Repos), true
}

// ProjectSlugs lists, sorted, the projects the file has a table for.
func (c *Config) ProjectSlugs() []string {
	if c == nil {
		return nil
	}
	var out []string
	for slug := range c.projects {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// AllProjects returns the all-projects settings ([defaults]), Defaults
// filled in: what a project follows for each key it doesn't set.
func (c *Config) AllProjects() (Safety, error) {
	s := Defaults
	if c == nil {
		return s, nil
	}
	// The older top-level default_agent named the coordinators' agent;
	// [defaults] coordinator_agent wins over it.
	if c.agent != "" {
		s.CoordinatorAgent = c.agent
	}
	// Activating, pausing, archiving or pinning is a project's own
	// state: in [defaults] it would stop, hide or pin every project.
	if c.defaults.Active != nil || c.defaults.Paused != nil || c.defaults.Archived != nil || c.defaults.Pinned != nil {
		return s, fmt.Errorf("%s: defaults can't set active, paused, archived or pinned; they are each project's own", c.Path)
	}
	// Sharing is opted into project by project: all projects' share
	// would offer every project, new ones too.
	if c.defaults.Share != nil {
		return s, fmt.Errorf("%s: defaults can't set share; each project is shared on its own", c.Path)
	}
	if c.defaults.Repos != nil {
		return s, fmt.Errorf("%s: defaults can't set repos; each project's repos are its own", c.Path)
	}
	if c.defaults.Links != nil {
		return s, fmt.Errorf("%s: defaults.links: links are per project", c.Path)
	}
	return s, c.defaults.apply(&s, c.Path, "defaults")
}

// Own lists the keys slug's table sets itself (ProjectKeys order),
// auto_resolve as auto_close; the rest follow all projects.
func (c *Config) Own(slug string) []string {
	if c == nil {
		return nil
	}
	r, ok := c.projects[slug]
	if !ok {
		return nil
	}
	return r.own()
}

// OwnDefaults lists the keys [defaults] sets (ProjectKeys order),
// auto_resolve as auto_close: the all-projects settings the human chose,
// where the rest are Defaults.
func (c *Config) OwnDefaults() []string {
	if c == nil {
		return nil
	}
	return c.defaults.own()
}

// Removed lists, sorted, the projects whose complete_tasks still names
// the removed "released" (CompleteRemoved), read as CompleteUser; all
// projects ([defaults]) is AllProjectsName, first.
func (c *Config) Removed() []string {
	var out []string
	for slug, r := range c.projects {
		if r.CompleteTasks != nil && *r.CompleteTasks == CompleteRemoved {
			out = append(out, slug)
		}
	}
	sort.Strings(out)
	if r := c.defaults; r.CompleteTasks != nil && *r.CompleteTasks == CompleteRemoved {
		out = append([]string{AllProjectsName}, out...)
	}
	return out
}

// AllProjectsName is how the UI and tm doctor name [defaults].
const AllProjectsName = "all projects"
