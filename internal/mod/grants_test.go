package mod

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

func TestRunsAnything(t *testing.T) {
	for _, p := range []string{"sh", "bash", "python3", "python3.12", "Python.exe", "node", "env", "xargs", "osascript", "powershell", "pwsh.exe", "perl5.36", "sudo"} {
		if !RunsAnything(p) {
			t.Errorf("RunsAnything(%q) = false", p)
		}
	}
	for _, p := range []string{"git", "glab", "gh", "jq", "rg", "shellcheck", "nodemon", "python3-config"} {
		if RunsAnything(p) {
			t.Errorf("RunsAnything(%q) = true", p)
		}
	}
}

func TestCommandWords(t *testing.T) {
	p := Permissions{Commands: []string{"sh", "glab", "jq", "gh", "git"}}
	want := "runs jq, git; runs sh: anything it wants, as you; runs glab with your GitLab login; runs gh with your GitHub login"
	if got := p.PermWords(PermCommands); got != want {
		t.Errorf("PermWords(commands) = %q, want %q", got, want)
	}
	if got := (Permissions{Commands: []string{"jq"}}).PermWords(PermCommands); got != "runs jq" {
		t.Errorf("PermWords(commands) = %q", got)
	}
}

func withUserHome(t *testing.T, home string) {
	t.Helper()
	old := userHome
	userHome = func() string { return home }
	t.Cleanup(func() { userHome = old })
}

func TestHomeRoots(t *testing.T) {
	if caps.Windows {
		t.Skip("Unix paths")
	}
	withUserHome(t, "/Users/ada")
	for _, r := range []string{"/", "/Users", "/Users/ada", "/Users/ada/", "/Users/./ada", "~/", "~/.", "~//"} {
		if !holdsHome(r, "/Users/ada") {
			t.Errorf("holdsHome(%q) = false", r)
		}
	}
	for _, r := range []string{"~/notes", "/Users/ada/notes", "/Users/bob", "/opt/x", "~/x/."} {
		if holdsHome(r, "/Users/ada") {
			t.Errorf("holdsHome(%q) = true", r)
		}
	}
	m := &Manifest{Provides: Provides{Main: "main.ts"}, Permissions: Permissions{Files: Files{Read: []string{"/Users/ada", "/Users", "~/notes"}, Write: []string{"/Users/ada/notes"}}}}
	got := strings.Join(errStrings(m.checkPermissions()), "\n")
	for _, r := range []string{`"/Users/ada"`, `"/Users"`} {
		if !strings.Contains(got, r+" is the home folder") {
			t.Errorf("%s not refused: %s", r, got)
		}
	}
	if strings.Contains(got, "notes") {
		t.Errorf("a folder under home refused: %s", got)
	}
	words := Permissions{Files: Files{Read: []string{"/", "~/", "~/notes"}, Write: []string{"/Users"}}}.PermWords(PermFiles)
	want := "reads / (the whole disk, your home folder too), ~/ (everything in your home folder), ~/notes; writes /Users (your home folder and more)"
	if words != want {
		t.Errorf("PermWords(files) = %q, want %q", words, want)
	}
}
