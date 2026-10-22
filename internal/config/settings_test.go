package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestSettingKeys: the lists derived from the one table keep their keys
// and order.
func TestSettingKeys(t *testing.T) {
	want := []string{"start_threads", "yolo", "coordinator_approves", "parallel_threads", "global_parallel_threads", "auto_close", "auto_close_days", "auto_resolve", "pr_followup", "pr_poll_seconds", "complete_tasks", "coordinator_remote_control", "auto_clear", "coordinator_merges", "merge_method", "fast_forward_checkout", "protection", "coordinator_access", "thread_agent", "coordinator_agent", "models", "coordinator_model", "active", "paused", "archived", "pinned", "share",
		"archive_tasks_days", "archive_threads_days", "archive_inbox_days", "archive_journal_days", "link_burst", "link_per_hour", "link_max_kb", "branch_name", "guard", "guard_off", "guard_mode", "guard_allow"}
	if !slices.Equal(ProjectKeys, want) {
		t.Errorf("ProjectKeys %v", ProjectKeys)
	}
	if want := []string{"active", "paused", "archived", "pinned", "share"}; !slices.Equal(ProjectOnly, want) {
		t.Errorf("ProjectOnly %v", ProjectOnly)
	}
	if want := []string{"global_parallel_threads"}; !slices.Equal(DefaultsOnly, want) {
		t.Errorf("DefaultsOnly %v", DefaultsOnly)
	}
	for _, k := range ArchiveKeys {
		if !slices.Contains(ProjectKeys, k) {
			t.Errorf("archive key %s isn't a setting", k)
		}
	}
}

// TestSettingRowsCoverFields: every field of Safety and rawSafety (but
// repos, links, env and sandbox, which aren't settings) has its row, and Value reads
// each Safety field.
func TestSettingRowsCoverFields(t *testing.T) {
	s := reflect.ValueOf(Defaults)
	for i := range s.NumField() {
		key, _, _ := strings.Cut(s.Type().Field(i).Tag.Get("json"), ",")
		if _, ok := lookup(key); !ok {
			t.Errorf("Safety.%s (%s) has no row", s.Type().Field(i).Name, key)
		}
		if got := Defaults.Value(key); !reflect.DeepEqual(got, s.Field(i).Interface()) {
			t.Errorf("Value(%s) = %v, want %v", key, got, s.Field(i).Interface())
		}
	}
	r := reflect.TypeFor[rawSafety]()
	for i := range r.NumField() {
		key := r.Field(i).Tag.Get("toml")
		// sandbox_escapes is protection's older form, read by its row.
		if _, ok := lookup(key); !ok && key != "repos" && key != "links" && key != "env" && key != "sandbox" && key != "sandbox_escapes" {
			t.Errorf("rawSafety.%s (%s) has no row", r.Field(i).Name, key)
		}
	}
	if Defaults.Value("auto_resolve") != nil || Defaults.Value("repos") != nil {
		t.Error("Value of a key Safety has no field for")
	}
}

// TestCheckSettingAgreesWithLoad: what the writers refuse, Load refuses
// too, and the other way round, but for the removed "released", which
// Load reads as "user" and nothing writes.
func TestCheckSettingAgreesWithLoad(t *testing.T) {
	for _, tc := range []struct {
		key  string
		toml string
		v    any
	}{
		{"start_threads", `"auto"`, StartAuto}, {"start_threads", `"sometimes"`, "sometimes"},
		{"yolo", "true", true}, {"yolo", `"yes"`, "yes"},
		{"parallel_threads", "99", 99}, {"parallel_threads", "0", 0}, {"parallel_threads", "100", 100},
		{"auto_close", `"days"`, CloseDays}, {"auto_close", `"never"`, "never"},
		{"auto_close_days", "365", 365}, {"auto_close_days", "0", 0},
		{"auto_resolve", "false", false}, {"auto_resolve", "1", 1},
		{"pr_poll_seconds", "30", 30}, {"pr_poll_seconds", "5", 5},
		{"complete_tasks", `"merged"`, CompleteMerged}, {"complete_tasks", `"later"`, "later"},
		{"thread_agent", `"pi"`, "pi"}, {"thread_agent", `"two words"`, "two words"},
		{"coordinator_agent", `""`, ""},
		{"models", `["a", "b"]`, []string{"a", "b"}}, {"models", `["a", "a"]`, []string{"a", "a"}}, {"models", `[]`, []string{}},
		{"coordinator_model", `""`, ""}, {"coordinator_model", `"fake-big"`, "fake-big"}, {"coordinator_model", `"two words"`, "two words"},
		{"share", "true", true},
		{"archive_inbox_days", "30", 30}, {"archive_inbox_days", "400", 400},
	} {
		write(t, "[projects.demo]\n"+tc.key+" = "+tc.toml+"\n")
		_, loadErr := Load()
		checkErr := CheckSetting(tc.key, tc.v)
		if (loadErr == nil) != (checkErr == nil) {
			t.Errorf("%s = %s: Load %v, CheckSetting %v", tc.key, tc.toml, loadErr, checkErr)
		}
	}
	write(t, "[defaults]\nglobal_parallel_threads = -1\n")
	if _, err := Load(); err == nil || CheckSetting("global_parallel_threads", -1) == nil {
		t.Error("global_parallel_threads -1 taken")
	}
	write(t, "[projects.demo]\ncomplete_tasks = \"released\"\n")
	if _, err := Load(); err != nil || CheckSetting("complete_tasks", CompleteRemoved) == nil {
		t.Errorf("released: Load %v, and CheckSetting takes it", err)
	}
}

// TestSettingErrors: Load names the table, the key and the file's value;
// CheckSetting the setting in words.
func TestSettingErrors(t *testing.T) {
	for body, want := range map[string]string{
		"[projects.demo]\nparallel_threads = 0\n":          "projects.demo.parallel_threads must be 1 to 99, not 0",
		"[projects.demo]\nstart_threads = \"sometimes\"\n": `projects.demo.start_threads must be "propose" or "auto", not "sometimes"`,
		"[defaults]\nauto_close = \"never\"\n":             `defaults.auto_close must be "off", "merged" or "days", not "never"`,
		"[defaults]\nglobal_parallel_threads = -1\n":       "defaults.global_parallel_threads must be 0 (no cap) to 999, not -1",
		"[projects.demo]\nthread_agent = \"a b\"\n":        `projects.demo.thread_agent must be one agent's name, not "a b"`,
		"[projects.demo]\nmodels = []\n":                   "projects.demo.models must name at least one model",
		"[projects.demo]\nmerge = \"anyone\"\n":            `projects.demo.merge must be "coordinator" or "thread", not "anyone"`,
		"[defaults]\nguard_off = [\"push\"]\n":             `defaults.guard_off names an unknown rule "push"`,
		"[projects.demo]\ncoordinator_model = \"a b\"\n":   `projects.demo.coordinator_model is refused: model name "a b"`,
	} {
		write(t, body)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
	for _, tc := range []struct {
		key  string
		v    any
		want string
	}{
		{"parallel_threads", 0, "parallel threads must be 1 to 99"},
		{"parallel_threads", "4", "parallel_threads must be a number"},
		{"start_threads", "x", `start threads must be "propose" or "auto"`},
		{"archive_tasks_days", 0, "archive days must be 1 to 365"},
		{"models", "a", "models must be a list of names"},
		{"models", nil, "models must be a list of names"},
		{"coordinator_agent", "a b", `the agent must be one agent's name, not "a b"`},
		{"coordinator_model", "a b", `the coordinator model is refused: model name "a b"`},
		{"merge", MergeThread, `unknown setting "merge"`},
		{"repos", []string{"x"}, `unknown setting "repos"`},
	} {
		if err := CheckSetting(tc.key, tc.v); err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("CheckSetting(%s, %#v) = %v, want %q", tc.key, tc.v, err, tc.want)
		}
	}
}

// TestAutoCloseOverAutoResolve: auto_close wins over the older
// auto_resolve, which alone still sets it, and Own lists either as
// auto_close.
func TestAutoCloseOverAutoResolve(t *testing.T) {
	write(t, "[projects.a]\nauto_resolve = false\nauto_close = \"days\"\n\n[projects.b]\nauto_resolve = false\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("a"); s.AutoClose != CloseDays {
		t.Errorf("a: %s", s.AutoClose)
	}
	if s, _ := c.Safety("b"); s.AutoClose != CloseOff {
		t.Errorf("b: %s", s.AutoClose)
	}
	for _, slug := range []string{"a", "b"} {
		if own := c.Own(slug); !slices.Equal(own, []string{"auto_close"}) {
			t.Errorf("%s: Own %v", slug, own)
		}
	}
}

// TestSafetySet: Set changes the field Value reads, and only with a
// value of its type.
func TestSafetySet(t *testing.T) {
	s := Defaults
	for key, v := range map[string]any{"yolo": true, "parallel_threads": 3, "auto_close": CloseDays, "models": []string{"a"}, "archive_inbox_days": 9, "guard_off": []string{"merge"}} {
		s.Set(key, v)
		if got := s.Value(key); !reflect.DeepEqual(got, v) {
			t.Errorf("%s: %v, want %v", key, got, v)
		}
	}
	if s.ArchiveDays("archive_inbox_days") != 9 || s.ArchiveDays("parallel_threads") != 0 {
		t.Errorf("ArchiveDays %d %d", s.ArchiveDays("archive_inbox_days"), s.ArchiveDays("parallel_threads"))
	}
	before := s
	s.Set("parallel_threads", "4")
	s.Set("auto_resolve", true)
	s.Set("nope", 1)
	if !reflect.DeepEqual(s, before) {
		t.Errorf("changed: %+v", s)
	}
}
