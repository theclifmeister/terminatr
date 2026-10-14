package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home"
)

func write(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	if body != "" {
		os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600)
	}
}

func TestDefaultsWithoutFile(t *testing.T) {
	write(t, "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("demo"); !reflect.DeepEqual(s, Defaults) {
		t.Fatalf("got %+v", s)
	}
}

func TestProjectSettings(t *testing.T) {
	write(t, `
detach_key = "ctrl-\\"   # another package's setting: ignored here

[projects.demo]
start_threads = "auto"
yolo = true
complete_tasks = "merged"

[projects.other]
coordinator_approves = false
auto_resolve = false
pr_followup = false
coordinator_remote_control = true
auto_clear = true
coordinator_merges = true
fast_forward_checkout = false
sandbox_escapes = false
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("demo"); !reflect.DeepEqual(s, Safety{StartThreads: "auto", Yolo: true, CoordinatorApproves: true, ParallelThreads: 10, AutoClose: "merged", AutoCloseDays: 7, PRFollowup: true, PRPollSeconds: 120, CompleteTasks: "merged", FastForwardCheckout: true, SandboxEscapes: true, Merge: "coordinator", Guard: true, Active: true, ArchiveTasksDays: 30, ArchiveThreadsDays: 30, ArchiveInboxDays: 30, ArchiveJournalDays: 30}) {
		t.Fatalf("demo %+v", s)
	}
	if s, _ := c.Safety("other"); !reflect.DeepEqual(s, Safety{StartThreads: "propose", ParallelThreads: 10, AutoClose: "off", AutoCloseDays: 7, PRPollSeconds: 120, CompleteTasks: "user", CoordinatorRemoteControl: true, AutoClear: true, CoordinatorMerges: true, Merge: "coordinator", Guard: true, Active: true, ArchiveTasksDays: 30, ArchiveThreadsDays: 30, ArchiveInboxDays: 30, ArchiveJournalDays: 30}) {
		t.Fatalf("other %+v", s)
	}
}

// TestCompleteReleasedRemoved: the removed complete_tasks "released" is
// read as "user", so nothing completes silently, and Removed names the
// project for tm doctor.
func TestCompleteReleasedRemoved(t *testing.T) {
	write(t, "[projects.b]\ncomplete_tasks = \"released\"\n\n[projects.a]\ncomplete_tasks = \"released\"\n\n[projects.c]\ncomplete_tasks = \"merged\"\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("a"); s.CompleteTasks != CompleteUser {
		t.Fatalf("a %+v", s)
	}
	if got := strings.Join(c.Removed(), " "); got != "a b" {
		t.Fatalf("Removed = %q", got)
	}
	if err := SetProject("a", "complete_tasks", CompleteRemoved); err == nil {
		t.Fatal("wrote the removed value")
	}
}

// TestAutoCloseAndCap: auto_close wins over the older auto_resolve, and
// the number settings take their own values.
func TestAutoCloseAndCap(t *testing.T) {
	write(t, `
[projects.a]
auto_resolve = false
auto_close = "days"
auto_close_days = 3
parallel_threads = 4

[projects.b]
auto_resolve = true
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("a"); s.AutoClose != CloseDays || s.AutoCloseDays != 3 || s.ParallelThreads != 4 {
		t.Fatalf("a %+v", s)
	}
	if s, _ := c.Safety("b"); s.AutoClose != CloseMerged || s.ParallelThreads != 10 {
		t.Fatalf("b %+v", s)
	}
}

func TestBadSettings(t *testing.T) {
	for body, want := range map[string]string{
		"[projects.demo]\nstart_threads = \"sometimes\"\n": "start_threads must be",
		"[projects.demo]\nyoloo = true\n":                  "unknown setting projects.demo.yoloo",
		"[projects.demo]\nyolo = \"yes\"\n":                "yolo",
		"not toml":                                         "config.toml",
		"[projects.demo]\nparallel_threads = 0\n":          "parallel_threads must be 1 to 99",
		"[projects.demo]\nauto_close = \"never\"\n":        "auto_close must be",
		"[projects.demo]\nauto_close_days = 0\n":           "auto_close_days must be 1 to 365",
		"[projects.demo]\narchive_inbox_days = 0\n":        "archive_inbox_days must be 1 to 365",
		"[defaults]\narchive_journal_days = 400\n":         "defaults.archive_journal_days must be 1 to 365",
		"[projects.demo]\ncomplete_tasks = \"later\"\n":    "complete_tasks must be",
		"[defaults]\nyoloo = true\n":                       "unknown setting defaults.yoloo",
		"[defaults]\narchived = true\n":                    "defaults can't set active, paused, archived or pinned",
		"[defaults]\npaused = false\n":                     "defaults can't set active, paused, archived or pinned",
		"[defaults]\nactive = false\n":                     "defaults can't set active, paused, archived or pinned",
		"[defaults]\npinned = true\n":                      "defaults can't set active, paused, archived or pinned",
		"[defaults]\nshare = true\n":                       "defaults can't set share",
		"[projects.demo]\nshare = \"yes\"\n":               "share",
		"[defaults]\nparallel_threads = 100\n":             "defaults.parallel_threads must be 1 to 99",
		"[defaults]\nglobal_parallel_threads = -1\n":       "defaults.global_parallel_threads must be 0 (no cap) to 999",
		"[projects.demo]\nglobal_parallel_threads = 5\n":   "projects.demo can't set global_parallel_threads",
		"[defaults]\nauto_close = \"never\"\n":             "defaults.auto_close must be",
		"[projects.demo]\nmerge = \"anyone\"\n":            "merge must be",
		"[defaults]\nguard_off = [\"push\"]\n":             "unknown rule \"push\"",
		"[projects.demo]\nguard = \"no\"\n":                "guard",
	} {
		write(t, body)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err %v, want %q", body, err, want)
		}
	}
}

// TestKeysAndUI: [keys] and [ui] are read here too, the older [keys]
// detach as the prefix, and their unknown keys are listed, not errors.
func TestKeysAndUI(t *testing.T) {
	write(t, "[keys]\ndetach = \"ctrl+a\"\nprefx = \"ctrl+q\"\n\n[ui]\nicon = \"nerd\"\nicons = \"ascii\"\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Prefix != "ctrl+a" || c.Icons != "ascii" || strings.Join(c.Unknown, ",") != "keys.prefx,ui.icon" {
		t.Fatalf("prefix %q icons %q unknown %v", c.Prefix, c.Icons, c.Unknown)
	}
	write(t, "[keys]\nprefix = \"ctrl+o\"\ndetach = \"ctrl+a\"\n")
	if c, _ = Load(); c.Prefix != "ctrl+o" {
		t.Fatalf("prefix %q, want prefix over detach", c.Prefix)
	}
	// A bad project setting fails Load, but the TUI still gets its keys.
	write(t, "[keys]\nprefix = \"ctrl+o\"\n[projects.demo]\nyoloo = true\n")
	if c, err = Load(); err == nil || c == nil || c.Prefix != "ctrl+o" {
		t.Fatalf("bad project: %v, %+v", err, c)
	}
}

// TestAllProjects: each setting is the project's own value, else the
// all-projects one ([defaults]), else the built-in default; a project
// without a table follows all projects in everything.
func TestAllProjects(t *testing.T) {
	write(t, `
[defaults]
start_threads = "auto"
parallel_threads = 4
auto_close = "off"
complete_tasks = "merged"
archive_threads_days = 14
sandbox_escapes = false

[projects.demo]
parallel_threads = 2
sandbox_escapes = true
complete_tasks = "user"
archive_journal_days = 90

[projects.old]
auto_resolve = true
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	all := Safety{StartThreads: "auto", CoordinatorApproves: true, ParallelThreads: 4, AutoClose: "off", AutoCloseDays: 7, PRFollowup: true, PRPollSeconds: 120, CompleteTasks: "merged", FastForwardCheckout: true, Merge: "coordinator", Guard: true, Active: true,
		ArchiveTasksDays: 30, ArchiveThreadsDays: 14, ArchiveInboxDays: 30, ArchiveJournalDays: 30}
	if s, _ := c.AllProjects(); !reflect.DeepEqual(s, all) {
		t.Fatalf("all projects %+v", s)
	}
	if s, _ := c.Safety("new"); !reflect.DeepEqual(s, all) {
		t.Fatalf("a new project %+v, want all projects", s)
	}
	want := all
	want.ParallelThreads, want.CompleteTasks, want.ArchiveJournalDays, want.SandboxEscapes = 2, "user", 90, true
	if s, _ := c.Safety("demo"); !reflect.DeepEqual(s, want) {
		t.Fatalf("demo %+v", s)
	}
	// A project's older auto_resolve overrides the defaults' auto_close.
	if s, _ := c.Safety("old"); s.AutoClose != CloseMerged || s.ParallelThreads != 4 {
		t.Fatalf("old %+v", s)
	}
	if got := strings.Join(c.Own("demo"), " "); got != "parallel_threads complete_tasks sandbox_escapes archive_journal_days" {
		t.Fatalf("Own(demo) = %q", got)
	}
	if got := strings.Join(c.Own("old"), " "); got != "auto_close" {
		t.Fatalf("Own(old) = %q", got)
	}
	if got := c.Own("new"); got != nil {
		t.Fatalf("Own(new) = %q", got)
	}
}

// TestAllProjectsReleasedRemoved: "released" in [defaults] is read as
// "user" and named first by Removed.
func TestAllProjectsReleasedRemoved(t *testing.T) {
	write(t, "[defaults]\ncomplete_tasks = \"released\"\n\n[projects.a]\ncomplete_tasks = \"released\"\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("b"); s.CompleteTasks != CompleteUser {
		t.Fatalf("b %+v", s)
	}
	if got := strings.Join(c.Removed(), ","); got != "all projects,a" {
		t.Fatalf("Removed = %q", got)
	}
}

// TestAgentMod: [agent_mod] enabled and pane are off unless set, band on unless
// set false; its unknown keys are listed, not errors.
func TestAgentMod(t *testing.T) {
	write(t, "")
	if c, _ := Load(); c.AgentMod || !c.AgentModBand || c.AgentModPane {
		t.Fatalf("without a file: mods %v, band %v, pane %v", c.AgentMod, c.AgentModBand, c.AgentModPane)
	}
	write(t, "[agent_mod]\nenabled = true\nsidebar = true\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.AgentMod || !c.AgentModBand || c.AgentModPane || strings.Join(c.Unknown, ",") != "agent_mod.sidebar" {
		t.Fatalf("mods %v, band %v, pane %v, unknown %v", c.AgentMod, c.AgentModBand, c.AgentModPane, c.Unknown)
	}
	write(t, "[agent_mod]\nenabled = true\nband = false\n")
	if c, err := Load(); err != nil || !c.AgentMod || c.AgentModBand || c.AgentModPane || len(c.Unknown) != 0 {
		t.Fatalf("band = false: %v, %+v", err, c)
	}
	write(t, "[agent_mod]\nenabled = true\npane = true\n")
	if c, err := Load(); err != nil || !c.AgentModBand || !c.AgentModPane || len(c.Unknown) != 0 {
		t.Fatalf("pane = true: %v, %+v", err, c)
	}
}

// TestAgentModAlias: the older [mods] enabled, band and pane still count
// for each key [agent_mod] doesn't set; [mods.<id>] tables are user
// mods', read into Mods, with their unknown keys listed, and a stray
// scalar under [mods] is unknown too.
func TestAgentModAlias(t *testing.T) {
	write(t, "[mods]\nenabled = true\nband = false\npane = true\n")
	if c, err := Load(); err != nil || !c.AgentMod || c.AgentModBand || !c.AgentModPane || len(c.Unknown) != 0 {
		t.Fatalf("alias: %v, %+v", err, c)
	}
	write(t, "[mods]\nenabled = true\nband = false\n[agent_mod]\nenabled = false\n")
	if c, err := Load(); err != nil || c.AgentMod || c.AgentModBand {
		t.Fatalf("agent_mod wins per key: %v, %+v", err, c)
	}
	write(t, "[mods.nord]\nenabled = true\ntheme = \"x\"\n\n[mods]\nsidebar = 1\n")
	if c, err := Load(); err != nil || c.AgentMod || !c.Mods["nord"].Enabled || strings.Join(c.Unknown, ",") != "mods.nord.theme,mods.sidebar" {
		t.Fatalf("[mods.<id>]: %v, %+v", err, c)
	}
}

func TestModelsAllowList(t *testing.T) {
	write(t, `
[defaults]
models = ["opus", "sonnet"]

[projects.demo]
models = ["opus"]

[projects.other]
yolo = true
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("demo"); !reflect.DeepEqual(s.Models, []string{"opus"}) || !s.AllowsModel("opus") || s.AllowsModel("sonnet") {
		t.Fatalf("demo: %v", s.Models)
	}
	if s, _ := c.Safety("other"); !reflect.DeepEqual(s.Models, []string{"opus", "sonnet"}) || s.AllowsModel("haiku") {
		t.Fatalf("other: %v", s.Models)
	}
	if got := c.Own("demo"); !reflect.DeepEqual(got, []string{"models"}) {
		t.Fatalf("own: %v", got)
	}
	if s, _ := (*Config)(nil).AllProjects(); s.Models != nil || !s.AllowsModel("haiku") {
		t.Fatal("no setting allows every model")
	}
	for _, bad := range []string{`models = []`, `models = ["a b"]`, `models = ["opus", "opus"]`, `models = "opus"`} {
		write(t, "[defaults]\n"+bad+"\n")
		if _, err := Load(); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// TestGuard: merge, guard and guard_off default to the coordinator
// merging and every rule on; a project's own value wins over [defaults].
func TestGuard(t *testing.T) {
	write(t, "[defaults]\nguard_off = [\"credentials\"]\n[projects.a]\nmerge = \"thread\"\nguard = false\n[projects.b]\nguard_off = []\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("a"); s.Merge != MergeThread || s.Guard || !reflect.DeepEqual(s.GuardOff, []string{"credentials"}) {
		t.Errorf("a %+v", s)
	}
	if s, _ := c.Safety("b"); s.Merge != MergeCoordinator || !s.Guard || len(s.GuardOff) != 0 {
		t.Errorf("b %+v", s)
	}
	if s, _ := c.Safety("c"); s.Merge != MergeCoordinator || !s.Guard || !reflect.DeepEqual(s.GuardOff, []string{"credentials"}) {
		t.Errorf("c %+v", s)
	}
}

// TestContextHint: [ui] context_hint is 40 unless set; 0 is never, and
// a percent past 100 is an error.
func TestContextHint(t *testing.T) {
	write(t, "")
	if c, _ := Load(); c.ContextHint != DefaultContextHint || DefaultContextHint != 40 {
		t.Fatalf("without a file: %d", c.ContextHint)
	}
	write(t, "[ui]\nicons = \"ascii\"\n")
	if c, err := Load(); err != nil || c.ContextHint != 40 || len(c.Unknown) != 0 {
		t.Fatalf("unset: %v %+v", err, c)
	}
	write(t, "[ui]\ncontext_hint = 55\n")
	if c, err := Load(); err != nil || c.ContextHint != 55 || len(c.Unknown) != 0 {
		t.Fatalf("55: %v %+v", err, c)
	}
	write(t, "[ui]\ncontext_hint = 0\n")
	if c, err := Load(); err != nil || c.ContextHint != 0 {
		t.Fatalf("0 means never: %v %+v", err, c)
	}
	write(t, "[ui]\ncontext_hint = 140\n")
	if _, err := Load(); err == nil {
		t.Fatal("140% loaded")
	}
}

// TestPRPollSeconds: pr_poll_seconds defaults to 2 minutes, follows
// [defaults] and a project's own table key by key, and 30 seconds is
// the least.
func TestPRPollSeconds(t *testing.T) {
	write(t, `
[defaults]
pr_poll_seconds = 60
[projects.demo]
pr_poll_seconds = 45
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("demo"); s.PRPollSeconds != 45 {
		t.Errorf("project's own: %d", s.PRPollSeconds)
	}
	if s, _ := c.Safety("other"); s.PRPollSeconds != 60 {
		t.Errorf("defaults: %d", s.PRPollSeconds)
	}
	for _, bad := range []string{"29", "0", "3601"} {
		write(t, "[defaults]\npr_poll_seconds = "+bad+"\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "pr_poll_seconds must be 30 to 3600") {
			t.Errorf("%s: %v", bad, err)
		}
	}
	write(t, "")
	c, _ = Load()
	if s, _ := c.Safety("demo"); s.PRPollSeconds != 120 {
		t.Errorf("default: %d", s.PRPollSeconds)
	}
}

// TestUpdateCheck: [ui] update_check is on unless set false.
func TestUpdateCheck(t *testing.T) {
	write(t, "")
	if c, _ := Load(); !c.UpdateCheck {
		t.Fatal("off without a file")
	}
	write(t, "[ui]\nicons = \"ascii\"\n")
	if c, err := Load(); err != nil || !c.UpdateCheck {
		t.Fatalf("unset: %v %+v", err, c)
	}
	write(t, "[ui]\nupdate_check = false\n")
	if c, err := Load(); err != nil || c.UpdateCheck || len(c.Unknown) != 0 {
		t.Fatalf("false: %v %+v", err, c)
	}
}

// TestModsTables: [mods.<id>] reads enabled, grants, projects and
// settings; a table with a key of the wrong type is that mod's Err and
// doesn't fail Load; an older [mods] key of the wrong type still does.
func TestModsTables(t *testing.T) {
	write(t, "[mods.review-age]\nenabled = true\ngrants = [\"project_read\"]\nprojects = [\"demo\"]\n[mods.review-age.settings]\nwarn_after_hours = 48\n\n[mods.nord]\nenabled = \"yes\"\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	ra := c.Mods["review-age"]
	if !ra.Enabled || strings.Join(ra.Grants, ",") != "project_read" || strings.Join(ra.Projects, ",") != "demo" || ra.Settings["warn_after_hours"] != int64(48) || ra.Err != nil {
		t.Fatalf("review-age %+v", ra)
	}
	if n := c.Mods["nord"]; n.Enabled || n.Err == nil || len(c.Unknown) != 0 {
		t.Fatalf("nord %+v, unknown %v", n, c.Unknown)
	}
	// A mod's settings set before anything else of it: its table is
	// there, implied.
	write(t, "[mods.nord.settings]\ndark = true\n")
	if c, err := Load(); err != nil || c.Mods["nord"].Settings["dark"] != true || len(c.Unknown) != 0 {
		t.Fatalf("implied table: %v %+v %v", err, c.Mods, c.Unknown)
	}
	write(t, "[mods]\nenabled = \"yes\"\n")
	if _, err := Load(); err == nil {
		t.Fatal("a bad older [mods] enabled passed")
	}
}

// TestRepos (T335): a project's repo list is its table's repos, never
// [defaults]', and no settings write reaches it; SetRepos and
// UpdateRepos write it, an empty one too, and RenameTable moves it.
func TestRepos(t *testing.T) {
	write(t, "[projects.demo]\nyolo = true # mine\nrepos = [\"/src/a\"]\n\n[projects.other]\nyolo = false\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, set := c.Repos("demo"); !set || !reflect.DeepEqual(got, []string{"/src/a"}) {
		t.Fatalf("demo: %v %v", got, set)
	}
	if got, set := c.Repos("other"); set || got != nil {
		t.Fatalf("other: %v %v", got, set)
	}
	if got, set := (*Config)(nil).Repos("demo"); set || got != nil {
		t.Fatalf("nil: %v %v", got, set)
	}
	if own := c.Own("demo"); !reflect.DeepEqual(own, []string{"yolo"}) {
		t.Fatalf("own %v: repos isn't a setting", own)
	}
	if err := SetProject("demo", "repos", []string{"/x"}); err == nil {
		t.Fatal("SetProject wrote repos")
	}
	if err := UnsetProject("demo", "repos"); err == nil {
		t.Fatal("UnsetProject removed repos")
	}
	if err := UpdateRepos("other", func(old []string, set bool) ([]string, error) {
		if set || old != nil {
			t.Errorf("other's list: %v %v", old, set)
		}
		return []string{"/src/b"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetRepos("demo", nil); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got, set := c.Repos("demo"); !set || len(got) != 0 {
		t.Fatalf("demo emptied: %v %v", got, set)
	}
	if got, _ := c.Repos("other"); !reflect.DeepEqual(got, []string{"/src/b"}) {
		t.Fatalf("other: %v", got)
	}
	data, _ := os.ReadFile(c.Path)
	if !strings.Contains(string(data), "yolo = true # mine") {
		t.Fatalf("the user's line changed:\n%s", data)
	}
	out, err := RenameTable(data, "other", "renamed")
	if err != nil || !strings.Contains(string(out), "[projects.renamed]\nyolo = false\nrepos = [\"/src/b\"]") {
		t.Fatalf("rename: %v\n%s", err, out)
	}

	write(t, "[defaults]\nrepos = [\"/src/a\"]\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "defaults can't set repos") {
		t.Fatalf("defaults repos: %v", err)
	}
}
