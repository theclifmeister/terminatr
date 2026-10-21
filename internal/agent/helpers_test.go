package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestLaunchRepoValues: launch templates see the repo root and the
// worktree's own git dir, and read the brief as a TOML-quoted value
// (Codex's -c developer_instructions=…, T97).
func TestLaunchRepoValues(t *testing.T) {
	brief := filepath.Join(t.TempDir(), "brief.md")
	text := "# T1 \"quoted\"\n\tC:\\path\x01\x7f é\n"
	os.WriteFile(brief, []byte(text), 0o600)
	m, err := ParseManifest([]byte(`manifest_version = 1
name = "a"
[launch]
command = "a"
args = ["-c", 'projects={ {{- toml .RepoRoot}}={trust_level="trusted"} }', "--write", "{{.GitDir}}",
  "-c", "developer_instructions={{toml (file .BriefPath)}}", "{{json (file .BriefPath)}}"]
`))
	if err != nil {
		t.Fatal(err)
	}
	spec := threadSpec()
	spec.Cwd, spec.RepoRoot, spec.GitDir, spec.BriefPath = "/r/wt", "/r/repo", "/r/repo/.git/worktrees/wt", brief
	l, err := FromManifest(m).Launch(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Argv[2]; got != `projects={"/r/repo"={trust_level="trusted"} }` {
		t.Errorf("trust arg %q", got)
	}
	if l.Argv[4] != spec.GitDir {
		t.Errorf("git dir arg %q", l.Argv[4])
	}
	var v struct {
		DeveloperInstructions string `toml:"developer_instructions"`
	}
	if _, err := toml.Decode(l.Argv[6], &v); err != nil || v.DeveloperInstructions != text {
		t.Errorf("developer_instructions %q -> %q %v", l.Argv[6], v.DeveloperInstructions, err)
	}
	if want := "\"# T1 \\\"quoted\\\"\\n\\tC:\\\\path\\u0001\x7f é\\n\""; l.Argv[7] != want {
		t.Errorf("json brief %s, want %s", l.Argv[7], want)
	}

	spec.BriefPath = filepath.Join(t.TempDir(), "missing.md")
	if _, err := FromManifest(m).Launch(spec); err == nil {
		t.Error("a missing file must fail the launch")
	}
	spec.BriefPath = "brief.md"
	if _, err := FromManifest(m).Launch(spec); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("a relative path: %v", err)
	}
	big := filepath.Join(t.TempDir(), "big.md")
	os.WriteFile(big, make([]byte, maxFileText+1), 0o600)
	spec.BriefPath = big
	if _, err := FromManifest(m).Launch(spec); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("a file over the bound: %v", err)
	}
}

// TestTOMLString: every string comes back from a TOML decoder as it went
// in (invalid UTF-8 as U+FFFD).
func TestTOMLString(t *testing.T) {
	for _, s := range []string{"", "plain", `a "b" \c`, "\b\t\n\f\r\x00\x1f\x7f", "ünï 🙂", "''' \"\"\" ${x} {{y}}"} {
		var v struct{ K string }
		if _, err := toml.Decode("K = "+TOMLString(s), &v); err != nil || v.K != s {
			t.Errorf("%q -> %s -> %q %v", s, TOMLString(s), v.K, err)
		}
	}
	var v struct{ K string }
	if _, err := toml.Decode("K = "+TOMLString("a\xffb"), &v); err != nil || v.K != "a�b" {
		t.Errorf("invalid UTF-8 -> %q %v", v.K, err)
	}
}

// TestPathModes: one mode per path; a NoWrite beats a Write, a Write
// beats a Read.
func TestPathModes(t *testing.T) {
	a := Access{
		Read: []string{"/p", "/g"}, Write: []string{"/g", "/w", "/n"}, WriteFiles: []string{"/f", "/c"},
		NoWrite: []string{"/p", "/n"}, NoWriteFiles: []string{`/c "x".toml`, "/c"},
	}
	want := `{"/p"="read","/g"="write","/w"="write","/n"="read","/f"="write","/c"="read","/c \"x\".toml"="read"}`
	if got := pathModes(a); got != want {
		t.Errorf("pathModes = %s, want %s", got, want)
	}
	// A write dir beats a Read of the same path, not a NoWrite.
	b := Access{Read: []string{"/r", "/n"}, NoWrite: []string{"/n"}}
	if got, want := pathModes(b, "/r", "/n", "/x"), `{"/r"="write","/n"="read","/x"="write"}`; got != want {
		t.Errorf("pathModes with write dirs = %s, want %s", got, want)
	}
	// A write dir goes by its real path, as the policy names its own.
	dir := t.TempDir()
	real, _ := filepath.EvalSymlinks(dir)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if got, want := pathModes(Access{Read: []string{real}}, link), "{"+TOMLString(real)+`="write"}`; got != want {
		t.Errorf("pathModes via a link = %s, want %s", got, want)
	}
	if got := pathModes(Access{}); got != "{}" {
		t.Errorf("empty policy = %s", got)
	}
}

// codexUsage is the [jsonl_tail] a Codex manifest would carry: usage
// from event_msg/token_count, one turn per task_complete.
const codexUsage = `manifest_version = 1
name = "codex"
[launch]
command = "codex"
[jsonl_tail]
path_field = "transcript_path"
[[jsonl_tail.rules]]
match = { type = "event_msg", "payload.type" = "task_complete" }
state = "idle"
[jsonl_tail.usage]
match = { type = "event_msg", "payload.type" = "token_count" }
input = "payload.info.last_token_usage.input_tokens"
cache_read = "payload.info.last_token_usage.cached_input_tokens"
output = "payload.info.last_token_usage.output_tokens"
input_includes_cache = true
context = "payload.info.last_token_usage.input_tokens"
context_window = "payload.info.model_context_window"
plan_percent = "payload.rate_limits.primary.used_percent"
key = "payload.info.total_token_usage.total_tokens"
turn = { type = "event_msg", "payload.type" = "task_complete" }
`

// Lines in the shape of a Codex 0.160 rollout (~/.codex/sessions/…/
// rollout-*.jsonl), numbers made up: a turn with two requests, where the
// first token_count has no info yet and the second request's count is
// written twice.
var codexLines = []string{
	`{"timestamp":"2026-10-06T20:18:01.002Z","type":"event_msg","payload":{"type":"task_started","model_context_window":258400}}`,
	`{"timestamp":"2026-10-06T20:18:01.010Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"primary":{"used_percent":3.0,"window_minutes":300,"resets_in_seconds":9000},"secondary":{"used_percent":1.0,"window_minutes":10080,"resets_in_seconds":500000}}}}`,
	`{"timestamp":"2026-10-06T20:18:04.551Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":12034,"cached_input_tokens":3456,"output_tokens":210,"reasoning_output_tokens":128,"total_tokens":12244},"last_token_usage":{"input_tokens":12034,"cached_input_tokens":3456,"output_tokens":210,"reasoning_output_tokens":128,"total_tokens":12244},"model_context_window":258400},"rate_limits":{"primary":{"used_percent":3.0,"window_minutes":300,"resets_in_seconds":8996}}}}`,
	`{"timestamp":"2026-10-06T20:18:09.120Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":24890,"cached_input_tokens":15232,"output_tokens":512,"reasoning_output_tokens":256,"total_tokens":25402},"last_token_usage":{"input_tokens":12856,"cached_input_tokens":11776,"output_tokens":302,"reasoning_output_tokens":128,"total_tokens":13158},"model_context_window":258400},"rate_limits":{"primary":{"used_percent":4.0,"window_minutes":300,"resets_in_seconds":8991}}}}`,
	`{"timestamp":"2026-10-06T20:18:09.121Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":24890,"cached_input_tokens":15232,"output_tokens":512,"reasoning_output_tokens":256,"total_tokens":25402},"last_token_usage":{"input_tokens":12856,"cached_input_tokens":11776,"output_tokens":302,"reasoning_output_tokens":128,"total_tokens":13158},"model_context_window":258400},"rate_limits":{"primary":{"used_percent":4.0,"window_minutes":300,"resets_in_seconds":8991}}}}`,
	`{"timestamp":"2026-10-06T20:18:09.300Z","type":"event_msg","payload":{"type":"task_complete","last_agent_message":"Done."}}`,
	`{"timestamp":"2026-10-06T20:18:09.301Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done."}]}}`,
}

// TestTailUsageCodex: the usage table reads Codex's token_count lines:
// the uncached input, the cache reads, the output and the context, and
// a turn from task_complete. Repeats are the tailer's to drop, by Key.
func TestTailUsageCodex(t *testing.T) {
	m, err := ParseManifest([]byte(codexUsage))
	if err != nil {
		t.Fatal(err)
	}
	tail := m.JSONLTail
	want := []*Usage{
		nil,
		nil, // no info yet
		{Turns: 0, Input: 12034 - 3456, CacheRead: 3456, Output: 210, Context: 12034, ContextWindow: 258400, PlanPct: 3, HasPlan: true, Key: "12244"},
		{Turns: 0, Input: 12856 - 11776, CacheRead: 11776, Output: 302, Context: 12856, ContextWindow: 258400, PlanPct: 4, HasPlan: true, Key: "25402"},
		{Turns: 0, Input: 12856 - 11776, CacheRead: 11776, Output: 302, Context: 12856, ContextWindow: 258400, PlanPct: 4, HasPlan: true, Key: "25402"},
		{Turns: 1},
		nil,
	}
	for i, line := range codexLines {
		l := tail.Parse([]byte(line))
		if (l.Usage == nil) != (want[i] == nil) || (l.Usage != nil && *l.Usage != *want[i]) {
			t.Errorf("line %d: usage %+v, want %+v", i, l.Usage, want[i])
		}
		if idle := l.HasSignal && l.Signal.State == StateIdle; idle != (i == 5) {
			t.Errorf("line %d: signal %+v %v", i, l.Signal, l.HasSignal)
		}
	}
}

// TestTailUsageTable: without a turn match every usage line is a turn;
// cost and the model are read; bad numbers read as 0; and the table is
// validated.
func TestTailUsageTable(t *testing.T) {
	u := &TailUsage{Match: map[string]string{"type": "usage"}, Input: "u.in", Output: "u.out",
		CacheCreation: "u.cw", CostUSD: "cost", Model: "model"}
	var obj map[string]any
	json.Unmarshal([]byte(`{"type":"usage","u":{"in":5,"out":-3,"cw":"7"},"cost":0.25,"model":"m-1"}`), &obj)
	if got := u.read(obj); got == nil || *got != (Usage{Turns: 1, Input: 5, CostUSD: 0.25, HasCost: true, Model: "m-1"}) {
		t.Errorf("usage line: %+v", got)
	}
	json.Unmarshal([]byte(`{"type":"other","u":{"in":5}}`), &obj)
	if got := u.read(obj); got != nil {
		t.Errorf("a line the match leaves out: %+v", got)
	}

	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n[jsonl_tail]\npath_field = \"p\"\n"
	for _, c := range []struct {
		table string
		ok    bool
	}{
		{"[jsonl_tail.usage]\nmatch = { t = \"u\" }\noutput = \"o\"\n", true},
		{"", false}, // neither rules nor usage
		{"[jsonl_tail.usage]\noutput = \"o\"\n", false},
		{"[jsonl_tail.usage]\nmatch = { t = \"u\" }\nmodel = \"m\"\n", false},
		{"[jsonl_tail.usage]\nmatch = { t = \"u\" }\ninput = \"i\"\ninput_includes_cache = true\n", false},
		{"[jsonl_tail.usage]\nmatch = { t = \"u\" }\noutput = \"o\"\nsurprise = 1\n", false},
	} {
		if _, err := ParseManifest([]byte(base + c.table)); (err == nil) != c.ok {
			t.Errorf("%q: err = %v, want ok %v", c.table, err, c.ok)
		}
	}
}

// Lines in the shape of a Claude Code 2.1.306 transcript, trimmed and
// numbers made up: a turn of two requests, the first written as two
// lines (one per content block) with the same message id and usage,
// then the turn's end; and a request of a model no window names.
var claudeLines = []string{
	`{"type":"user","message":{"role":"user","content":"Fix the bug."},"sessionId":"s1"}`,
	`{"type":"assistant","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":""}],"stop_reason":"tool_use","usage":{"input_tokens":2,"cache_creation_input_tokens":17073,"cache_read_input_tokens":27423,"output_tokens":201}},"sessionId":"s1"}`,
	`{"type":"assistant","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":2,"cache_creation_input_tokens":17073,"cache_read_input_tokens":27423,"output_tokens":201}},"sessionId":"s1"}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"sessionId":"s1"}`,
	`{"type":"assistant","requestId":"req_2","message":{"id":"msg_2","model":"claude-opus-5-5","content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn","usage":{"input_tokens":3,"cache_creation_input_tokens":800,"cache_read_input_tokens":44496,"output_tokens":40}},"sessionId":"s1"}`,
	`{"type":"system","subtype":"turn_duration","durationMs":5123,"sessionId":"s1"}`,
	`{"type":"assistant","requestId":"req_3","message":{"id":"msg_3","model":"claude-haiku-4-5","content":[{"type":"text","text":"Hi."}],"stop_reason":"end_turn","usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":5000,"output_tokens":4}},"sessionId":"s1"}`,
}

// TestTailUsageClaude: Claude's manifest reads usage from its
// transcript without the mod: the four counts, the context as the whole
// input (cache reads and writes too, as $.session.usage().context), the
// model's window from context_windows, the message id that tells a
// repeat, and a turn from turn_duration.
func TestTailUsageClaude(t *testing.T) {
	tail := claude(t).Sources().JSONLTail
	if tail == nil || tail.Usage == nil {
		t.Fatal("claude.toml has no [jsonl_tail.usage]")
	}
	first := &Usage{Input: 2, CacheCreation: 17073, CacheRead: 27423, Output: 201, Context: 2 + 17073 + 27423,
		ContextWindow: 1_000_000, Model: "claude-opus-5-5", Key: "msg_1"}
	want := []*Usage{
		nil,
		first,
		first, // the same request's next block: the tailer drops it by Key
		nil,
		{Input: 3, CacheCreation: 800, CacheRead: 44496, Output: 40, Context: 3 + 800 + 44496,
			ContextWindow: 1_000_000, Model: "claude-opus-5-5", Key: "msg_2"},
		{Turns: 1},
		{Input: 10, CacheRead: 5000, Output: 4, Context: 5010, Model: "claude-haiku-4-5", Key: "msg_3"},
	}
	for i, line := range claudeLines {
		l := tail.Parse([]byte(line))
		if (l.Usage == nil) != (want[i] == nil) || (l.Usage != nil && *l.Usage != *want[i]) {
			t.Errorf("line %d: usage %+v, want %+v", i, l.Usage, want[i])
		}
	}
	// Only fixed windows are listed: a model whose window may be either
	// (the 4.x 1M), or one not known yet, is left to the server's guess.
	for _, m := range []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-8", "claude-opus-6", "claude-opus-5-5-x"} {
		if w := tail.Usage.window(m); w != 0 {
			t.Errorf("%s: window %d, want none", m, w)
		}
	}
}

// TestTailUsageContextOptions: context_from_counts sums the counts in
// place of a context path; context_windows' longest matching pattern
// wins; both are validated.
func TestTailUsageContextOptions(t *testing.T) {
	u := &TailUsage{Match: map[string]string{"type": "u"}, Input: "in", CacheRead: "cr", ContextFromCounts: true,
		Model: "m", ContextWindows: map[string]int64{"a-*": 100, "a-long-*": 900}}
	line := func(s string) (obj map[string]any) {
		json.Unmarshal([]byte(s), &obj)
		return obj
	}
	if got := u.read(line(`{"type":"u","in":4,"cr":6,"m":"a-long-1"}`)); got == nil || got.Context != 10 || got.ContextWindow != 900 {
		t.Errorf("usage: %+v", got)
	}
	if got := u.read(line(`{"type":"u","in":4,"m":"a-1"}`)); got == nil || got.Context != 4 || got.ContextWindow != 100 {
		t.Errorf("usage: %+v", got)
	}
	if got := u.read(line(`{"type":"u","in":4,"m":"b"}`)); got == nil || got.ContextWindow != 0 {
		t.Errorf("a model no pattern names: %+v", got)
	}

	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n[jsonl_tail]\npath_field = \"p\"\n[jsonl_tail.usage]\nmatch = { t = \"u\" }\n"
	for table, ok := range map[string]bool{
		"input = \"i\"\ncontext_from_counts = true\n":                      true,
		"output = \"o\"\ncontext_from_counts = true\n":                     false, // no input
		"input = \"i\"\ncontext = \"c\"\ncontext_from_counts = true\n":     false, // both
		"input = \"i\"\n[jsonl_tail.usage.context_windows]\n\"m-*\" = 5\n": true,
		"input = \"i\"\n[jsonl_tail.usage.context_windows]\n\"m-*\" = 0\n": false,
		"input = \"i\"\n[jsonl_tail.usage.context_windows]\n\"m-[\" = 5\n": false,
	} {
		if _, err := ParseManifest([]byte(base + table)); (err == nil) != ok {
			t.Errorf("%q: err = %v, want ok %v", table, err, ok)
		}
	}
}

// TestJSONLTailAccepts: Claude's transcript is followed only under its
// projects folder and named for the session's id.
func TestJSONLTailAccepts(t *testing.T) {
	tail := claude(t).Sources().JSONLTail
	root := filepath.Join(string(filepath.Separator)+"h", ".claude")
	v := SourceVars{ConfigDir: root, AgentSID: "s1"}
	for p, ok := range map[string]bool{
		filepath.Join(root, "projects", "-r-p", "s1.jsonl"):                   true,
		filepath.Join(root, "projects", "-r-p", "s2.jsonl"):                   false,
		filepath.Join(root, "projects", "-r-p", "s1", "subagents", "a.jsonl"): false,
		filepath.Join(root, "projects", "..", "..", "etc", "s1.jsonl"):        false,
		filepath.Join(root, "other", "s1.jsonl"):                              false,
		filepath.Join("rel", "s1.jsonl"):                                      false,
		filepath.Join(string(filepath.Separator)+"tmp", "x", "s1.jsonl"):      false,
	} {
		if _, got := tail.Accepts(p, v); got != ok {
			t.Errorf("%s: %v, want %v", p, got, ok)
		}
	}
	if r, _ := tail.Accepts(filepath.Join(root, "projects", "-r-p", "s1.jsonl"), v); r != filepath.Join(root, "projects") {
		t.Errorf("opened under %q, want the projects folder", r)
	}
	if _, ok := tail.Accepts(filepath.Join(root, "projects", "-r-p", ".jsonl"), SourceVars{ConfigDir: root}); ok {
		t.Error("accepted with no session id")
	}
}
