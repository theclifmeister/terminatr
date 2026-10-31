package tui

import (
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
)

// TestSafetyRowKeys: each safety row, on the Settings and the Access
// tab, carries its own keys and value words (what scope reads, once
// zipped by position), and together they cover every setting the popups
// write.
func TestSafetyRowKeys(t *testing.T) {
	want := map[string][]string{
		"Start threads": {"start_threads"}, "Yolo mode": {"yolo"}, "Coordinator approves": {"coordinator_approves"},
		"Parallel threads": {"parallel_threads"}, "Auto-close finished threads": {"auto_close", "auto_close_days"},
		"Complete tasks": {"complete_tasks"}, "Pull request follow-up": {"pr_followup"}, "Remote control": {"coordinator_remote_control"},
		"Auto-clear coordinator": {"auto_clear"}, "Coordinator may merge": {"coordinator_merges"}, "Merge method": {"merge_method"}, "Keep my checkout current": {"fast_forward_checkout"},
		"Thread agent": {"thread_agent"}, "Coordinator agent": {"coordinator_agent"}, "Thread agents": {"thread_agents"}, "Thread models": {"models"},
		"Coordinator model": {"coordinator_model"}, "Branch names": {"branch_name"}, "Keep history": config.ArchiveKeys, "Link limits": config.LinkKeys,
		"Guard": {"guard"}, "Guard mode": {"guard_mode"}, "Guard rules": {"guard_off"}, "Protection": {"protection"}, "Coordinator sets access": {"coordinator_access"},
	}
	for _, slug := range []string{"demo", ""} {
		rows := append(safetySettings(slug), accessSettings(slug, config.OSMac)...)
		// auto_resolve is auto_close's older form; the popups leave
		// pr_poll_seconds to the file.
		// Guard allowances add to all projects' rather than follow
		// them: its list writes guard_allow (settings_guard.go).
		covered := map[string]bool{"auto_resolve": true, "pr_poll_seconds": true, "guard_allow": true}
		for _, r := range rows {
			if r.keys == nil {
				continue
			}
			if !slices.Equal(r.keys, want[r.label]) {
				t.Errorf("%q %s: keys %v, want %v", slug, r.label, r.keys, want[r.label])
			}
			if r.word == nil || r.word(config.Defaults) == "" {
				t.Errorf("%q %s: no value words", slug, r.label)
			}
			if slug == "" && (r.from != nil || r.unset != nil) || slug != "" && (r.from == nil || r.unset == nil) {
				t.Errorf("%q %s: scope not filled in", slug, r.label)
			}
			for _, k := range r.keys {
				covered[k] = true
			}
		}
		for _, r := range newSafetyRows(slug).historyRows() {
			if len(r.keys) != 1 || !slices.Contains(config.ArchiveKeys, r.keys[0]) || r.word == nil {
				t.Errorf("%q history %s: keys %v", slug, r.label, r.keys)
			}
		}
		for _, k := range config.ProjectKeys {
			if !covered[k] && !slices.Contains(config.ProjectOnly, k) && !slices.Contains(config.DefaultsOnly, k) {
				t.Errorf("%q: no row writes %s", slug, k)
			}
		}
		labels := map[string]bool{}
		for _, r := range rows {
			labels[r.label] = true
		}
		for _, l := range []string{"Threads across projects"} {
			if labels[l] != (slug == "") {
				t.Errorf("%q: %s shown %v", slug, l, labels[l])
			}
		}
		for _, l := range []string{"Active", "Paused", "Archive", "Delete"} {
			if labels[l] != (slug != "") {
				t.Errorf("%q: %s shown %v", slug, l, labels[l])
			}
		}
	}
}

// TestToggles: the on/off table names bool settings only.
func TestToggles(t *testing.T) {
	for k, tg := range toggles {
		if _, ok := config.Defaults.Value(k).(bool); !ok {
			t.Errorf("%s isn't an on/off setting", k)
		}
		if tg.label == "" || tg.help == "" || tg.what == "" {
			t.Errorf("%s: %+v", k, tg)
		}
	}
}

// TestBranchRow: the Branch names row shows the template and a preview
// of the next branch, with and without a task.
func TestBranchRow(t *testing.T) {
	sr := newSafetyRows("demo")
	if got := sr.branchPreview(config.DefaultBranchName); got != "tm/demo/t-0042-fix-login" {
		t.Errorf("default preview %q", got)
	}
	if got := sr.branchPreview("feature/{task}-{title}"); got != "feature/T42-fix-login (feature/fix-login without a task)" {
		t.Errorf("preview %q", got)
	}
	if got := newSafetyRows("").branchPreview("users/me/{id}"); got != "users/me/t-0042" {
		t.Errorf("preview %q", got)
	}
}
