package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/project"
)

// guardRow is the safety row labelled label of slug's settings.
func guardRow(t *testing.T, slug, label string) setting {
	t.Helper()
	for _, r := range safetySettings(slug) {
		if r.label == label {
			return r
		}
	}
	t.Fatalf("no row %q", label)
	return setting{}
}

// TestGuardSettingRows: the guard's rows show and change guard, its
// mode and its rules in the project's table; turning it off asks first.
func TestGuardSettingRows(t *testing.T) {
	src, m := popupData(t)
	if v := guardRow(t, "alpha", "Guard").value(m); v != "on" {
		t.Errorf("guard %q", v)
	}
	drain(t, m, guardRow(t, "alpha", "Guard mode").change(m))
	if !slices.Contains(src.settings, "projects.alpha.guard_mode=learn") {
		t.Errorf("mode: %v", src.settings)
	}
	if v := guardRow(t, "alpha", "Guard mode").value(m); v != "refuse, then ask you" {
		t.Errorf("mode shown %q", v)
	}
	// Off asks first; n leaves it on.
	if cmd := guardRow(t, "alpha", "Guard").change(m); cmd != nil {
		t.Fatal("turned off unasked")
	}
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("no question: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	if !slices.Contains(src.settings, "projects.alpha.guard=false") {
		t.Errorf("guard off: %v", src.settings)
	}

	// The rules: enter turns the selected one off, then on again.
	drain(t, m, guardRow(t, "alpha", "Guard rules").change(m))
	rv, ok := m.top().(*guardRulesView)
	if !ok {
		t.Fatalf("rules view: %T", m.top())
	}
	for range 5 {
		keyPress(m, "down")
	}
	if config.GuardRules[rv.sel] != "credentials" {
		t.Fatalf("sel %d", rv.sel)
	}
	drain(t, m, keyPress(m, "enter"))
	if out := screen(m); !strings.Contains(out, "Reading credentials") || !strings.Contains(out, "Guard rules for alpha") {
		t.Errorf("rules view:\n%s", out)
	}
	if v := guardRow(t, "alpha", "Guard rules").value(m); v != "5 of 6 on" {
		t.Errorf("rules shown %q", v)
	}
	drain(t, m, keyPress(m, "enter"))
	if !slices.Contains(src.settings, "projects.alpha.guard_off=[credentials]") || src.settings[len(src.settings)-1] != "projects.alpha.guard_off=[]" {
		t.Errorf("rules: %v", src.settings)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety("alpha"); s.Guard || s.GuardMode != config.GuardLearn || len(s.GuardOff) != 0 {
		t.Errorf("written %+v", s)
	}
}

// TestGuardAllowView: a project's list has all projects' allowances too,
// marked; x removes one from its own table, enter changes its text, and a
// credentials allowance can't become a pattern.
func TestGuardAllowView(t *testing.T) {
	src, m := popupData(t)
	os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(`[defaults]
guard_allow = [{ rule = "worktree-only", path = "~/src/shared/*" }]
[projects.alpha]
guard_allow = [{ rule = "force-push", command = "git push -f origin tm/*" }, { rule = "credentials", path = "~/.npmrc" }]
`), 0o600)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s, _ := c.Safety("alpha")
	all, _ := c.AllProjects()
	m.projectData("alpha").Safety, m.data.Defaults = &s, &all
	if v := guardRow(t, "alpha", "Guard allowances").value(m); v != "3 (1 of all projects)" {
		t.Errorf("value %q", v)
	}
	drain(t, m, guardRow(t, "alpha", "Guard allowances").change(m))
	out := screen(m)
	for _, want := range []string{"Threads write only in their worktree · all projects", "~/src/shared/*", "git push -f origin tm/*", "enter change · x remove"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	keyPress(m, "down")
	drain(t, m, keyPress(m, "x"))
	if got := src.settings[len(src.settings)-1]; got != "projects.alpha.guard_allow=[{credentials  ~/.npmrc false false}]" {
		t.Errorf("removed own: %v", src.settings)
	}
	if l := m.projectData("alpha").Safety.GuardAllow; len(l) != 2 || !l[0].All {
		t.Errorf("shown at once %+v", l)
	}
	// The all-projects one is removed from all projects'.
	keyPress(m, "up")
	drain(t, m, keyPress(m, "x"))
	if got := src.settings[len(src.settings)-1]; got != "defaults.guard_allow=<nil>" {
		t.Errorf("removed all projects': %v", src.settings)
	}
	// The credentials one stays exact.
	keyPress(m, "enter")
	in, ok := m.top().(*inputView)
	if !ok {
		t.Fatalf("no input: %T", m.top())
	}
	in.text = "~/.npm*"
	n := len(src.settings)
	drain(t, m, keyPress(m, "enter"))
	if len(src.settings) != n || !strings.Contains(m.msg, "no *") {
		t.Errorf("pattern taken: %v %q", src.settings, m.msg)
	}
}

// TestGuardAllowViewExact: an exact allowance is marked so, and stays
// exact when its text changes.
func TestGuardAllowViewExact(t *testing.T) {
	src, m := popupData(t)
	os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(`[projects.alpha]
guard_allow = [{ rule = "worktree-only", command = "cp build/* /opt/app/", exact = true }]
`), 0o600)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s, _ := c.Safety("alpha")
	m.projectData("alpha").Safety = &s
	drain(t, m, guardRow(t, "alpha", "Guard allowances").change(m))
	if out := screen(m); !strings.Contains(out, "Threads write only in their worktree · exact") {
		t.Errorf("not marked exact:\n%s", out)
	}
	keyPress(m, "enter")
	in, ok := m.top().(*inputView)
	if !ok {
		t.Fatalf("no input: %T", m.top())
	}
	if !strings.Contains(in.label, "exact") {
		t.Errorf("label %q", in.label)
	}
	in.text = "cp dist/* /opt/app/"
	drain(t, m, keyPress(m, "enter"))
	if got := src.settings[len(src.settings)-1]; got != "projects.alpha.guard_allow=[{worktree-only cp dist/* /opt/app/  true false}]" {
		t.Errorf("changed: %v", src.settings)
	}
}

// TestGuardRefusalsView: the refusals show whole commands; a allows for
// the project, telling the thread's running session; one the floor holds
// is never allowed, only kept refusing.
func TestGuardRefusalsView(t *testing.T) {
	src, m := popupData(t)
	now := time.Now()
	src.refusals = []guardlog.Entry{
		{ID: "r1", Project: "alpha", Thread: "t-0003", Session: "s-old", Rule: "force-push", Tool: "Bash", Summary: "git push with force", Subject: "git push --force-with-lease origin tm/x\nsecond line", Last: now, Count: 2},
		{ID: "r2", Project: "alpha", Thread: "t-0002", Rule: "merge", Tool: "Bash", Summary: "gh pr merge", Subject: "gh pr merge 7", Floor: true, Last: now},
		{ID: "r3", Project: "beta", Rule: "credentials", Tool: "Read", Subject: "/h/.npmrc", Path: true, Last: now},
	}
	drain(t, m, guardRow(t, "alpha", "Guard refusals").change(m))
	out := screen(m)
	for _, want := range []string{"t-0003 · force-pushing · Bash", "2 times", "git push --force-with-lease origin tm/x", "never allowed: a thread doesn't merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/h/.npmrc") {
		t.Errorf("another project's refusal listed:\n%s", out)
	}
	// enter shows it whole.
	keyPress(m, "enter")
	if out := screen(m); !strings.Contains(out, "second line") {
		t.Errorf("whole command:\n%s", out)
	}
	drain(t, m, keyPress(m, "a"))
	if !slices.Equal(src.decided, []string{"r1 here s-9"}) {
		t.Errorf("decided %v", src.decided)
	}
	// The floor's: a is refused, x keeps it.
	drain(t, m, keyPress(m, "A"))
	if len(src.decided) != 1 || !strings.Contains(m.msg, "never allows") {
		t.Errorf("floor allowed: %v %q", src.decided, m.msg)
	}
	drain(t, m, keyPress(m, "x"))
	if src.decided[1] != "r2 keep s-2" {
		t.Errorf("kept %v", src.decided)
	}
	if out := screen(m); !strings.Contains(out, "none to review") {
		t.Errorf("after both:\n%s", out)
	}
}

// TestGuardDecide: allowing writes the allowance to the project's own
// list (all projects' too stay), journals it without the command,
// marks the log and tells the session; keeping refusing stops listing
// it; the floor's and a credentials pattern are never allowed.
func TestGuardDecide(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("[defaults]\nguard_allow = [{ rule = \"worktree-only\", path = \"/x/*\" }]\n"), 0o600)
	const cmd = "curl -H 'Authorization: Bearer sekrit' https://registry/x"
	e, _, err := guardlog.Record(home, guardlog.Entry{Project: "demo", Thread: "t-0001", Rule: "credentials", Tool: "Bash", Summary: "an HTTP call with credentials", Subject: cmd}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var told []string
	tell := func(s string) { told = append(told, s) }
	who := caller.Caller{Kind: caller.Human}
	msg, err := guardDecide(who, home, e, guardAllowHere, tell)
	if err != nil || !strings.Contains(msg, "allowed for this project") {
		t.Fatalf("%q %v", msg, err)
	}
	c, _ := config.Load()
	s, _ := c.Safety("demo")
	if own := config.OwnAllows(s.GuardAllow); len(own) != 1 || own[0].Command != cmd || len(s.GuardAllow) != 2 {
		t.Errorf("allowances %+v", s.GuardAllow)
	}
	if len(told) != 1 || !strings.HasPrefix(told[0], "[tm] The user allowed the Bash call") || strings.Contains(told[0], "sekrit") {
		t.Errorf("told %q", told)
	}
	if l, _ := guardlog.List(home, "demo"); len(l) != 0 {
		t.Errorf("still listed %+v", l)
	}
	lines, _, _ := p.JournalTail(10)
	j := strings.Join(lines, "\n")
	if !strings.Contains(j, "guard.allow t-0001 credentials Bash: an HTTP call with credentials (this project)") || strings.Contains(j, "sekrit") {
		t.Errorf("journal %s", j)
	}
	// Allowing it again adds nothing.
	if _, err := guardDecide(who, home, e, guardAllowHere, tell); err != nil {
		t.Fatal(err)
	}
	c, _ = config.Load()
	if s, _ := c.Safety("demo"); len(s.GuardAllow) != 2 {
		t.Errorf("twice %+v", s.GuardAllow)
	}
	// All projects'.
	f, _, _ := guardlog.Record(home, guardlog.Entry{Project: "demo", Rule: "worktree-only", Tool: "Write", Subject: "/y/a.go", Path: true}, time.Now())
	if _, err := guardDecide(who, home, f, guardAllowAll, tell); err != nil {
		t.Fatal(err)
	}
	c, _ = config.Load()
	if all, _ := c.AllProjects(); len(all.GuardAllow) != 2 || all.GuardAllow[1].Path != "/y/a.go" {
		t.Errorf("all projects %+v", all.GuardAllow)
	}
	// Kept refusing.
	k, _, _ := guardlog.Record(home, guardlog.Entry{Project: "demo", Rule: "force-push", Tool: "Bash", Subject: "git push -f"}, time.Now())
	if msg, err := guardDecide(who, home, k, guardKeep, tell); err != nil || !strings.Contains(msg, "keeps refusing") {
		t.Errorf("keep %q %v", msg, err)
	}
	if l, _ := guardlog.List(home, ""); len(l) != 0 {
		t.Errorf("listed %+v", l)
	}
	// Never: the floor's, and a credentials subject with a *.
	if _, err := guardDecide(who, home, guardlog.Entry{Project: "demo", Rule: "merge", Floor: true}, guardAllowHere, tell); err == nil {
		t.Error("floor allowed")
	}
	if _, err := guardDecide(who, home, guardlog.Entry{Project: "demo", Rule: "credentials", Subject: "cat ~/.ssh/*"}, guardAllowHere, tell); err == nil {
		t.Error("credentials pattern allowed")
	}
	// A * in a call allowed is the call's own: the allowance is exact.
	g, _, _ := guardlog.Record(home, guardlog.Entry{Project: "demo", Rule: "worktree-only", Tool: "Bash", Subject: "cp build/* /opt/app/"}, time.Now())
	if _, err := guardDecide(who, home, g, guardAllowHere, tell); err != nil {
		t.Fatal(err)
	}
	c, _ = config.Load()
	s, _ = c.Safety("demo")
	if own := config.OwnAllows(s.GuardAllow); !slices.Contains(own, config.GuardAllow{Rule: "worktree-only", Command: "cp build/* /opt/app/", Exact: true}) {
		t.Errorf("not exact %+v", own)
	}
}
