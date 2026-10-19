package config

import (
	"slices"
	"strings"
	"testing"
)

// TestCheckSocket: a socket is one file, its path written whole, never
// in tm's folder.
func TestCheckSocket(t *testing.T) {
	for _, e := range []string{"~/.colima/default/docker.sock", "/var/run/docker.sock", "~/pg.sock", "/tmp/mysql.sock"} {
		if err := CheckSocket(e); err != nil {
			t.Errorf("%q refused: %v", e, err)
		}
	}
	for e, why := range map[string]string{
		"docker.sock":                 "from the root",
		"~/run/*.sock":                "pattern",
		"/var/run/../docker.sock":     "plainly",
		"/var/run/":                   "plainly",
		"~/$SOCK":                     "$",
		"/docker.sock":                "top level",
		"~/.terminatr/run/human.sock": "tm's folder",
		"~/.TERMINATR/run/tm.sock":    "",
		" /var/run/docker.sock":       "spaces",
	} {
		err := CheckSocket(e)
		if why == "" {
			continue // case folding depends on the platform
		}
		if err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v, want %q", e, err, why)
		}
	}
	if w := socketWarning("~/.colima/default/docker.sock"); !strings.Contains(w, "any container") {
		t.Errorf("docker warning: %s", w)
	}
	if w := socketWarning("~/pg.sock"); !strings.Contains(w, "outside the sandbox") || strings.Contains(w, "container") {
		t.Errorf("plain warning: %s", w)
	}
}

// TestApprovedCommands: the form, wide patterns taken with a warning, tm
// itself refused, and
// word-for-word matching with a last * taking more.
func TestApprovedCommands(t *testing.T) {
	for _, e := range []string{"make test", "make e2e", "npm run e2e *", "docker compose up *", "go test ./...", "./scripts/check.sh"} {
		if err := CheckApprovedCommand(e); err != nil {
			t.Errorf("%q refused: %v", e, err)
		}
	}
	// A wide pattern is the human's to set (T619): taken, with its
	// warning.
	for e, warn := range map[string]string{
		"make *":       "make runs other programs",
		"go *":         "every argument of go",
		"prlctl *":     "every argument of prlctl",
		"sh -c *":      "runs the code",
		"python3 -c *": "runs the code",
		"npx *":        "runs other programs",
		"npm run *":    "runs the code",
		"env sh *":     "runs other programs",
		"make test *":  "any argument",
		"make test":    "any code as you",
	} {
		if err := CheckApprovedCommand(e); err != nil {
			t.Errorf("%q refused: %v", e, err)
		}
		if w := SandboxWarning(SandboxApproved, e); !strings.Contains(w, warn) {
			t.Errorf("%q warns %q, want %q", e, w, warn)
		}
	}
	for e, why := range map[string]string{
		"make test*":        "pattern inside",
		"npm run 'e2e'":     "quote",
		"make $T":           "$",
		"make test | tee x": "|",
		"make  test":        "one space",
		"tm report":         "tm itself",
		"env tm *":          "tm itself",
		"sudo -n tm report": "tm itself",
		"*":                 "names no program",
		"":                  "empty",
	} {
		if why == "" {
			if err := CheckApprovedCommand(e); err != nil {
				t.Errorf("%q refused: %v", e, err)
			}
			continue
		}
		if err := CheckApprovedCommand(e); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v, want %q", e, err, why)
		}
	}
	for _, c := range []struct {
		e    string
		argv []string
		ok   bool
	}{
		{"make test", []string{"make", "test"}, true},
		{"make test", []string{"make", "test", "X=1"}, false},
		{"make test", []string{"make"}, false},
		{"npm run e2e *", []string{"npm", "run", "e2e"}, true},
		{"npm run e2e *", []string{"npm", "run", "e2e", "--", "--grep", "a b"}, true},
		{"npm run e2e *", []string{"npm", "run", "e2e2"}, false},
		{"make test", []string{"make", "test;", "rm"}, false},
		{"make test", nil, false},
	} {
		if got := ApprovedMatch(c.e, c.argv); got != c.ok {
			t.Errorf("ApprovedMatch(%q, %q) = %v", c.e, c.argv, got)
		}
	}
	// A Codex thread's excluded commands: those that fit.
	if v, why := ApprovedOf("npx playwright *"); v != "npx playwright *" || why != "" {
		t.Errorf("npx playwright *: %q %q", v, why)
	}
	if v, why := ApprovedOf("npm run test:*"); v != "" || !strings.Contains(why, "Codex") {
		t.Errorf("npm run test:*: %q %q", v, why)
	}
}

// TestSocketsAndApprovedLists: both lists add down the layers, drops
// and refusals as the others.
func TestSocketsAndApprovedLists(t *testing.T) {
	write(t, `
[defaults.sandbox]
sockets = ["~/pg.sock"]
approved_commands = ["make test", "make $T"]

[projects.web.sandbox]
sockets = ["/var/run/docker.sock"]
approved_commands = ["make e2e"]
drop = ["make test"]
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	ts := c.ThreadSandbox("web")
	if got := ts.Given(SandboxSockets); !slices.Equal(got, []string{"~/pg.sock", "/var/run/docker.sock"}) {
		t.Errorf("sockets %q", got)
	}
	if got := ts.Given(SandboxApproved); !slices.Equal(got, []string{"make e2e"}) {
		t.Errorf("approved %q", got)
	}
	if i := slices.IndexFunc(ts.Rules, func(r SandboxRule) bool { return r.Value == "make $T" }); i < 0 || ts.Rules[i].Refused == "" {
		t.Errorf("make $T not refused: %+v", ts.Rules)
	}
	if err := EditSandbox("projects.web", func(t *SandboxTable) error { t.Add(SandboxApproved, "tm *"); return nil }); err == nil {
		t.Error("tm * added")
	}
	if err := EditSandbox("projects.web", func(t *SandboxTable) error { t.Add(SandboxSockets, "~/redis.sock"); return nil }); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got := c.ThreadSandbox("web").Given(SandboxSockets); !slices.Contains(got, "~/redis.sock") {
		t.Errorf("after edit %q", got)
	}
}
