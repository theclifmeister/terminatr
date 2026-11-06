package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmokeThreadPromptFile: tm thread prompt --file puts the file in the
// project's uploads/ and its path in the thread's prompt, keeps it in the
// thread's follow-ups and the journal, and stores nothing for a prompt that
// isn't sent.
func TestSmokeThreadPromptFile(t *testing.T) {
	env, projDir, _ := tickerEnv(t)
	th := startThread(t, env, projDir)
	src := filepath.Join(t.TempDir(), "brief notes.md")
	if err := os.WriteFile(src, []byte("# Notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uploads := func() []string {
		ents, _ := os.ReadDir(filepath.Join(projDir, "uploads"))
		var out []string
		for _, e := range ents {
			out = append(out, e.Name())
		}
		return out
	}

	out := env.MustCLI("thread", "prompt", "t-0001", "read this", "--file", src, "--project", "demo")
	got := uploads()
	if len(got) != 1 || !strings.HasSuffix(got[0], "-brief-notes.md") {
		t.Fatalf("uploads: %v", got)
	}
	path := filepath.Join(projDir, "uploads", got[0])
	if !strings.Contains(out, "attached "+path) {
		t.Errorf("output: %q", out)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "# Notes\n" {
		t.Errorf("stored file: %q, %v", data, err)
	}
	if task, _ := os.ReadFile(filepath.Join(projDir, "threads", "t-0001", "task.md")); !strings.Contains(string(task), "read this\n\nAttached: "+path) {
		t.Errorf("task.md: %s", task)
	}
	if !Poll(agentWait, func() bool {
		return strings.Contains(strings.ReplaceAll(env.Screen(th), "\n", ""), "read this")
	}) {
		t.Errorf("the thread's screen lacks the prompt:\n%s", env.Screen(th))
	}
	journal, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	if !strings.Contains(string(journal), "thread.file t-0001 "+got[0]+" (8 bytes)") {
		t.Errorf("journal: %s", journal)
	}

	// A file alone is a prompt; a refused file is no prompt.
	env.WaitState(th, "idle", agentWait)
	env.MustCLI("thread", "prompt", "t-0001", "--file", src, "--project", "demo")
	if n := len(uploads()); n != 2 {
		t.Errorf("after a file alone: %v", uploads())
	}
	bad := filepath.Join(filepath.Dir(src), "run.sh")
	if err := os.WriteFile(bad, []byte("echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := env.CLI("thread", "prompt", "t-0001", "x", "--file", src, "--file", bad, "--project", "demo"); r.Code == 0 {
		t.Errorf("a refused type: %+v", r)
	}
	if n := len(uploads()); n != 2 {
		t.Errorf("a refused prompt stored: %v", uploads())
	}

	// A blocked thread takes no prompt, so stores no file.
	env.WaitState(th, "idle", agentWait)
	env.MustCLI("thread", "prompt", "t-0001", "run thread-block", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	if r := env.CLI("thread", "prompt", "t-0001", "x", "--file", src, "--project", "demo"); r.Code == 0 || !strings.Contains(r.Stderr, "blocked") {
		t.Errorf("while blocked: %+v", r)
	}
	if n := len(uploads()); n != 2 {
		t.Errorf("a blocked thread's prompt stored: %v", uploads())
	}
}
