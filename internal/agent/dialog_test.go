package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const permScreen = `  Bash command

    git push

  Do you want to proceed?
  ❯ 1. Yes
    2. Yes, and don't ask again for git push commands in /src
    3. No, and tell Claude what to do differently (esc)
`

func TestDialog(t *testing.T) {
	opt, q := DialogOption(permScreen, 1)
	if opt != "Yes" || q != "Do you want to proceed?" {
		t.Errorf("option 1: %q, %q", opt, q)
	}
	if opt, _ := DialogOption(permScreen, 4); opt != "" {
		t.Errorf("option 4: %q", opt)
	}
	want := []string{"Yes", "Yes, and don't ask again for git push commands in /src", "No, and tell Claude what to do differently (esc)"}
	if got := DialogOptions(permScreen); !slices.Equal(got, want) {
		t.Errorf("options %q", got)
	}
	if DialogOptions("no dialog here") != nil {
		t.Error("options without a dialog")
	}
	for opt, want := range map[string]bool{"Yes": false, want[1]: true, "Yes, always allow": true, "Yes, and don’t ask again": true, want[2]: false} {
		if AllowsAlways(opt) != want {
			t.Errorf("AllowsAlways(%q) = %v", opt, !want)
		}
	}
}

// TestOptionScope: each shipped agent's wording, as its dialogs show
// it, read off the versions the manifests name.
func TestOptionScope(t *testing.T) {
	cases := map[string]map[string]Scope{
		"claude": {
			"Yes":                          ScopeOnce,
			"Yes, but ask again next time": ScopeOnce,
			"No":                           ScopeOnce,
			"No, and tell Claude what to do differently (esc)":                             ScopeOnce,
			"No, and ask again next time":                                                  ScopeOnce,
			"Yes, allow reading from src/ during this session":                             ScopeSession,
			"Yes, allow reading from src/ and docs/ during this session (shift+tab)":       ScopeSession,
			"Yes, allow reading from src/ from this project":                               ScopePersistent,
			"Yes, and don't ask again for git push commands in /src":                       ScopePersistent,
			"Yes, and don’t ask again for example.com":                                     ScopePersistent,
			"Yes, and always allow access to src/ from this project":                       ScopePersistent,
			"Yes, and allow access to src/ and git commands":                               ScopePersistent,
			"Yes, and allow Claude to edit files in its ~/.claude folder for this session": ScopePersistent,
			"Yes, and switch to Accept edits for this session":                             ScopePersistent,
			"Yes, auto-accept edits":                                                       ScopePersistent,
			"Yes, and keep allowing any reads outside the working directories":             ScopePersistent,
			"No, and block reads outside the working directories from now on":              ScopePersistent,
			"Yes, and remember this directory":                                             ScopePersistent,
		},
		"codex": {
			"Yes, proceed (y)":    ScopeOnce,
			"Yes, just this once": ScopeOnce,
			"Allow":               ScopeOnce,
			"Yes, grant these permissions for this turn":      ScopeOnce,
			"No, and tell Codex what to do differently (esc)": ScopeOnce,
			"Cancel": ScopeOnce,
			"Yes, and don't ask again for this command in this session (a)":                                                   ScopeSession,
			"Yes, and allow this host for this conversation":                                                                  ScopeSession,
			"Yes, and allow these permissions for this session":                                                               ScopeSession,
			"Yes, grant these permissions for this session":                                                                   ScopeSession,
			"Allow for this session":                                                                                          ScopeSession,
			"Allow this request and remember this choice for this session":                                                    ScopeSession,
			"Yes, and don't ask again for commands that start with `git push` (p)":                                            ScopePersistent,
			"Yes, and don't ask again for commands that start with `echo x > f.txt && git add f.txt && git -c user.name=t -c": ScopePersistent,
			"Yes, and allow this host in the future":                                                                          ScopePersistent,
			"No, and block this host in the future":                                                                           ScopePersistent,
			"Allow and don't ask me again":                                                                                    ScopePersistent,
			"Always allow":                                                                                                    ScopePersistent,
			"Allow this request and remember this choice for future requests":                                                 ScopePersistent,
			"Yes, and don't ask again for these files":                                                                        ScopePersistent,
			"Yes, and allow it for every project":                                                                             ScopePersistent,
		},
	}
	for name, want := range cases {
		a, ok := BuiltinAgent(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		for opt, scope := range want {
			if got := OptionScope(ManifestOf(a), opt); got != scope {
				t.Errorf("%s: OptionScope(%q) = %s, want %s", name, opt, got, scope)
			}
		}
	}
	// Without wording: AllowsAlways alone, and no session.
	for opt, want := range map[string]Scope{"Yes": ScopeOnce, "Yes, during this session": ScopeOnce, "Yes, always": ScopePersistent} {
		if got := OptionScope(nil, opt); got != want {
			t.Errorf("no manifest: OptionScope(%q) = %s", opt, got)
		}
	}
}

// TestDialogScopes: the screens' options as the shipped wording reads
// them, so an option tm can't classify on a real screen shows here.
func TestDialogScopes(t *testing.T) {
	a, _ := BuiltinAgent("codex")
	for _, f := range []string{"t91-17-approval.screen", "t98-approval.screen", "t105-approval-wrapped.screen"} {
		b, err := os.ReadFile(filepath.Join("testdata", "codex", f))
		if err != nil {
			t.Fatal(err)
		}
		var got []Scope
		for _, o := range DialogOptions(string(b)) {
			got = append(got, OptionScope(ManifestOf(a), o))
		}
		if want := []Scope{ScopeOnce, ScopePersistent, ScopeOnce}; !slices.Equal(got, want) {
			t.Errorf("%s: %v", f, got)
		}
	}
	c, _ := BuiltinAgent("claude")
	var got []Scope
	for _, o := range DialogOptions(permScreen) {
		got = append(got, OptionScope(ManifestOf(c), o))
	}
	if want := []Scope{ScopeOnce, ScopePersistent, ScopeOnce}; !slices.Equal(got, want) {
		t.Errorf("claude: %v", got)
	}
}
