package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// TestPromptThroughShim: with codex an npm shim (codex.cmd), the text
// reaches it as one argument, cmd.exe's & > and % in it literal; a text
// with a line break or a quote isn't sent (the core pastes it).
func TestPromptThroughShim(t *testing.T) {
	a := load(t)
	target := agent.PromptTarget{SessionID: "s-1", AgentSID: "01a11c21-6753-78d3-acf7-54e1f5a71473"}
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	shim := "@echo off\r\n>\"%~dp0args\" echo(%*\r\n"
	if err := os.WriteFile(filepath.Join(dir, "codex.cmd"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(dir)

	text := "x & type nul > made.txt & echo 100%PATH%"
	if err := a.Prompt(context.Background(), target, text); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"--message=` + text + `"`; !strings.Contains(string(b), want) {
		t.Errorf("args %q, want %q in them", b, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "made.txt")); err == nil {
		t.Error("cmd.exe ran a command from the text")
	}

	os.Remove(args)
	for _, s := range []string{"first\nsecond", `say "hi"`} {
		if err := a.Prompt(context.Background(), target, s); err == nil {
			t.Errorf("%q sent", s)
		}
	}
	if _, err := os.Stat(args); err == nil {
		t.Error("codex.cmd ran")
	}
}
