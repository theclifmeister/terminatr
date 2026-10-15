package shell

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// With SHELL_TEST_ARGS set the test binary is node: it writes its
// arguments there as JSON.
func TestMain(m *testing.M) {
	if p := os.Getenv("SHELL_TEST_ARGS"); p != "" {
		b, _ := json.Marshal(os.Args[1:])
		os.WriteFile(p, b, 0o600)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// writeNpmShim makes dir an npm bin folder: codex.cmd as npm writes it,
// the script it names and, as node.exe beside it, this test binary. It
// returns the script.
func writeNpmShim(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, `node_modules\@openai\codex\bin\codex.js`)
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex.cmd"), []byte(npmShim), 0o700); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.Create(filepath.Join(dir, "node.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	return script
}

// TestCLICommandNpmShim: an npm shim runs as node <script> with the
// arguments as they are, cmd.exe nowhere.
func TestCLICommandNpmShim(t *testing.T) {
	dir := t.TempDir()
	script := writeNpmShim(t, dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(dir)
	out := filepath.Join(dir, "args.json")
	t.Setenv("SHELL_TEST_ARGS", out)

	args := []string{`--message=x" & type nul > made.txt & "`, "100%PATH%", "first\nsecond", `permissions={tm={network={mode="limited"}}}`}
	if err := CLICommand(context.Background(), "codex", args...).Run(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if want := append([]string{script}, args...); !slices.Equal(got, want) {
		t.Errorf("args\n got %q\nwant %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "made.txt")); err == nil {
		t.Error("cmd.exe ran a command from the arguments")
	}

	s, err := BatchStart(filepath.Join(dir, "codex.cmd"), []string{"a"})
	if err != nil || s.Line != "" || s.App != filepath.Join(dir, "node.exe") {
		t.Errorf("BatchStart = %+v, %v", s, err)
	}
}
