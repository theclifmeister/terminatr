package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
)

// sandboxData is popupData with all projects' sandbox lists, as Load
// reads them; alpha sets none of its own.
func sandboxData(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	src, m := popupData(t)
	os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(`[defaults.sandbox]
excluded_commands = ["docker compose *"]
`), 0o600)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.data.Sandbox = map[string]map[string][]string{"": c.SandboxOf(""), "alpha": c.SandboxOf("alpha")}
	return src, m
}

// TestSandboxView: a project follows all projects' list until it is
// changed there; adding an excluded command asks first, saying it runs
// outside the sandbox; a match-all entry is refused; f follows all
// projects again.
func TestSandboxView(t *testing.T) {
	src, m := sandboxData(t)
	row := guardRow(t, "alpha", "Sandbox: excluded commands")
	if v, from := row.value(m), row.from(m); v != "1" || from != config.AllProjectsName {
		t.Errorf("value %q from %q", v, from)
	}
	drain(t, m, row.change(m))
	out := screen(m)
	for _, want := range []string{"outside the sandbox", "full file and network access", "docker compose *", "All projects' list, which alpha follows", "f follow all projects"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	enter := func(text string) {
		t.Helper()
		in, ok := m.top().(*inputView)
		if !ok {
			t.Fatalf("no input: %T", m.top())
		}
		in.text = text
		drain(t, m, keyPress(m, "enter"))
	}
	keyPress(m, "a")
	enter("*")
	if len(src.sandbox) != 0 || !strings.Contains(m.msg, "matches every command") {
		t.Errorf("* taken: %v %q", src.sandbox, m.msg)
	}
	keyPress(m, "a")
	enter("npx playwright *")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "outside the sandbox") {
		t.Fatalf("no warning before adding: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	// The project's own list starts from all projects'.
	if want := []string{`projects.alpha excluded_commands ["docker compose *" "npx playwright *"]`}; !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %v", src.sandbox)
	}
	if row.from(m) != "" {
		t.Error("still follows all projects")
	}
	// A broad entry's warning is put too, and listed under it.
	keyPress(m, "a")
	enter("bash *")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "runs other programs") {
		t.Fatalf("no broad warning: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	if !strings.Contains(screen(m), "runs other programs") {
		t.Errorf("warning not listed:\n%s", screen(m))
	}
	// x removes the selected (the last added), f follows all projects.
	drain(t, m, keyPress(m, "x"))
	c, _ := config.Load()
	if got, _ := c.ThreadSandbox("alpha"); !reflect.DeepEqual(got.ExcludedCommands, []string{"docker compose *", "npx playwright *"}) {
		t.Errorf("written %+v", got)
	}
	drain(t, m, keyPress(m, "f"))
	c, _ = config.Load()
	if got := c.SandboxOf("alpha"); len(got) != 0 {
		t.Errorf("still its own: %v", got)
	}
	if row.from(m) != config.AllProjectsName {
		t.Error("doesn't follow all projects")
	}
}

// TestSandboxAllowAll: an allow rule without a warning is added at
// once, to all projects'; removing the last one leaves none set.
func TestSandboxAllowAll(t *testing.T) {
	src, m := sandboxData(t)
	drain(t, m, guardRow(t, "", "Sandbox: allow rules").change(m))
	keyPress(m, "a")
	m.top().(*inputView).text = "Bash(npx playwright *)"
	drain(t, m, keyPress(m, "enter"))
	keyPress(m, "a")
	m.top().(*inputView).text = "Bash"
	drain(t, m, keyPress(m, "enter"))
	if !strings.Contains(m.msg, "allows every Bash call") {
		t.Errorf("msg %q", m.msg)
	}
	drain(t, m, keyPress(m, "x"))
	if want := []string{`defaults allow ["Bash(npx playwright *)"]`, `defaults allow []`}; !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %v", src.sandbox)
	}
	c, _ := config.Load()
	if _, ok := c.SandboxOf("")[config.SandboxAllow]; ok {
		t.Error("an empty all-projects list is still set")
	}
}
