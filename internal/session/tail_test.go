package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// TestReadTailUsage: the tailer hands on the usage of whole lines
// appended since the file was named, once each: a repeat of the last
// report (same key) and a partial last line are not counted (the latter
// until it is whole).
func TestReadTailUsage(t *testing.T) {
	m, err := agent.ParseManifest([]byte(`manifest_version = 1
name = "a"
[launch]
command = "a"
[jsonl_tail]
path_field = "transcript_path"
[jsonl_tail.usage]
match = { type = "token_count" }
input = "last.input"
cache_read = "last.cached"
input_includes_cache = true
context = "last.input"
context_window = "window"
key = "total"
turn = { type = "task_complete" }
`))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := newAgentRT(AgentConfig{Agent: agent.FromManifest(m)}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "rollout.jsonl")
	appendLines := func(s string) {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(s)
		f.Close()
	}
	appendLines(`{"type":"token_count","last":{"input":999},"total":999}` + "\n") // before the session named it
	rt.setTail(filepath.Dir(p), p)
	appendLines(`{"type":"token_count","last":{"input":100,"cached":40},"window":1000,"total":100}` + "\n" +
		`{"type":"token_count","last":{"input":100,"cached":40},"window":1000,"total":100}` + "\n" +
		`{"type":"token_count","last":{"input":300,"cached":250},"window":1000,"total":400}` + "\n" +
		`{"type":"task_complete"}` + "\n" +
		`{"type":"token_count","last":{"input":7`)
	got, _ := rt.readTail()
	want := []agent.Usage{
		{Input: 60, CacheRead: 40, Context: 100, ContextWindow: 1000, Key: "100"},
		{Input: 50, CacheRead: 250, Context: 300, ContextWindow: 1000, Key: "400"},
		{Turns: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("usage %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("usage[%d] %+v, want %+v", i, got[i], want[i])
		}
	}
	// A cost reported by any of them stays reported in the merge.
	if m := mergeUsage([]agent.Usage{{Input: 1}, {Input: 2, CostUSD: 0.5, HasCost: true}}); !m.HasCost || m.CostUSD != 0.5 || m.Input != 3 {
		t.Errorf("merged cost: %+v", m)
	}
	// A tick hands them on as one report: one write of the record.
	if m, w := mergeUsage(got), (agent.Usage{Turns: 1, Input: 110, CacheRead: 290, Context: 300, ContextWindow: 1000, Key: "400"}); m != w {
		t.Errorf("merged %+v, want %+v", m, w)
	}
	// The partial line, whole now; then a repeat of it across reads.
	appendLines(`00,"cached":0},"window":1000,"total":1100}` + "\n")
	if got, _ := rt.readTail(); len(got) != 1 || got[0].Input != 700 {
		t.Fatalf("completed line: %+v", got)
	}
	appendLines(`{"type":"token_count","last":{"input":700,"cached":0},"window":1000,"total":1100}` + "\n")
	if got, _ := rt.readTail(); len(got) != 0 {
		t.Fatalf("repeat across reads: %+v", got)
	}
}

// TestReadTailLongLine: a line longer than one read is skipped, however
// many reads it spans, and the lines after it are read.
func TestReadTailLongLine(t *testing.T) {
	m, err := agent.ParseManifest([]byte(`manifest_version = 1
name = "a"
[launch]
command = "a"
[jsonl_tail]
path_field = "transcript_path"
[jsonl_tail.usage]
match = { type = "token_count" }
input = "n"
key = "n"
`))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := newAgentRT(AgentConfig{Agent: agent.FromManifest(m)}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rt.setTail(filepath.Dir(p), p)
	appendLines := func(s string) {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(s)
		f.Close()
	}
	read := func() []agent.Usage {
		t.Helper()
		var got []agent.Usage
		for range 4 {
			u, _ := rt.readTail()
			got = append(got, u...)
		}
		return got
	}
	long := `{"type":"token_count","n":1,"pad":"` + strings.Repeat("x", 2*maxTailRead) + `"}`
	appendLines(long[:3*maxTailRead/2]) // still being written
	if got := read(); len(got) != 0 {
		t.Fatalf("partial long line: %+v", got)
	}
	appendLines(long[3*maxTailRead/2:] + "\n" + `{"type":"token_count","n":2}` + "\n")
	if got := read(); len(got) != 1 || got[0].Input != 2 {
		t.Fatalf("after the long line: %+v", got)
	}
	appendLines(`{"type":"token_count","n":3}` + "\n")
	if got := read(); len(got) != 1 || got[0].Input != 3 {
		t.Fatalf("next line: %+v", got)
	}
}

// TestReadTailOpensUnderRoot: the followed file is read only through
// its root: not when it is a link, nor when a folder on the way links
// out of the root, though the link's target has the lines.
func TestReadTailOpensUnderRoot(t *testing.T) {
	m, err := agent.ParseManifest([]byte(`manifest_version = 1
name = "a"
[launch]
command = "a"
[jsonl_tail]
path_field = "transcript_path"
[jsonl_tail.usage]
match = { type = "token_count" }
input = "n"
`))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "s1.jsonl")
	os.WriteFile(outside, nil, 0o600)
	root := t.TempDir()
	os.Symlink(outside, filepath.Join(root, "s1.jsonl"))
	os.Symlink(filepath.Dir(outside), filepath.Join(root, "dir"))
	for _, p := range []string{filepath.Join(root, "s1.jsonl"), filepath.Join(root, "dir", "s1.jsonl")} {
		rt, err := newAgentRT(AgentConfig{Agent: agent.FromManifest(m)}, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		rt.setTail(root, p)
		f, _ := os.OpenFile(outside, os.O_APPEND|os.O_WRONLY, 0o600)
		f.WriteString(`{"type":"token_count","n":5}` + "\n")
		f.Close()
		if got, _ := rt.readTail(); len(got) != 0 {
			t.Errorf("%s: read through a link: %+v", p, got)
		}
	}
	// The same file named directly under its own root is read.
	rt, _ := newAgentRT(AgentConfig{Agent: agent.FromManifest(m)}, 0, false)
	rt.setTail(filepath.Dir(outside), outside)
	f, _ := os.OpenFile(outside, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"type":"token_count","n":6}` + "\n")
	f.Close()
	if got, _ := rt.readTail(); len(got) != 1 || got[0].Input != 6 {
		t.Errorf("direct: %+v", got)
	}
}
