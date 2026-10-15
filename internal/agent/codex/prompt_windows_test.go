package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// With CODEX_TEST_ARGS set the test binary is node: it writes its
// arguments there as JSON.
func TestMain(m *testing.M) {
	if p := os.Getenv("CODEX_TEST_ARGS"); p != "" {
		b, _ := json.Marshal(os.Args[1:])
		os.WriteFile(p, b, 0o600)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// npmShim is codex.cmd as npm writes it.
const npmShim = "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n\r\n" +
	"IF EXIST \"%dp0%\\node.exe\" (\r\n  SET \"_prog=%dp0%\\node.exe\"\r\n) ELSE (\r\n  SET \"_prog=node\"\r\n  SET PATHEXT=%PATHEXT:;.JS;=;%\r\n)\r\n\r\n" +
	"endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & \"%_prog%\"  \"%dp0%\\node_modules\\@openai\\codex\\bin\\codex.js\" %*\r\n"

// npmCodex puts codex.cmd as npm installs it first on PATH, with this test
// binary as its node.exe, and returns the args file its runs write and
// the script.
func npmCodex(t *testing.T) (args, script string) {
	t.Helper()
	dir := t.TempDir()
	script = filepath.Join(dir, `node_modules\@openai\codex\bin\codex.js`)
	os.MkdirAll(filepath.Dir(script), 0o700)
	os.WriteFile(script, nil, 0o600)
	os.WriteFile(filepath.Join(dir, "codex.cmd"), []byte(npmShim), 0o700)
	exe, _ := os.Executable()
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node.exe"), b, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(dir)
	args = filepath.Join(dir, "args.json")
	t.Setenv("CODEX_TEST_ARGS", args)
	return args, script
}

func readArgv(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	os.Remove(p)
	return got
}

// TestPromptThroughNpmShim: with codex npm's shim, Prompt runs node on
// its script, so any text, quotes and line breaks too, reaches it as it
// is.
func TestPromptThroughNpmShim(t *testing.T) {
	a := load(t)
	target := agent.PromptTarget{SessionID: "s-1", AgentSID: "01a11c21-6753-78d3-acf7-54e1f5a71473"}
	args, script := npmCodex(t)
	for _, text := range []string{`x" & type nul > made.txt & echo 100%PATH%`, "first\nsecond"} {
		if err := a.Prompt(context.Background(), target, text); err != nil {
			t.Fatal(err)
		}
		want := []string{script, "queue", "--thread=" + target.AgentSID, "--message=" + text}
		if got := readArgv(t, args); !slices.Equal(got, want) {
			t.Errorf("args\n got %q\nwant %q", got, want)
		}
	}
	if _, err := os.Stat("made.txt"); err == nil {
		t.Error("cmd.exe ran a command from the text")
	}
}

// TestProbeThroughNpmShim: the sandbox probe's profile flags, TOML with
// quotes, reach codex through npm's shim as they are.
func TestProbeThroughNpmShim(t *testing.T) {
	args, script := npmCodex(t)
	flags := []string{"-c", `permissions={tm={network={mode="limited"}}}`, "-c", `default_permissions="tm"`}
	if out, err := run("codex", flagsEnv(), flags...); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got, want := readArgv(t, args), append([]string{script}, flags...); !slices.Equal(got, want) {
		t.Errorf("args\n got %q\nwant %q", got, want)
	}
}

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
