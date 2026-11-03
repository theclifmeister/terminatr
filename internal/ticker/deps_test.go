package ticker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

func depItems(t *testing.T, f *project.Folder, kind string) []project.Item {
	t.Helper()
	items, err := f.Inbox()
	if err != nil {
		t.Fatal(err)
	}
	var out []project.Item
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

// TestSweepDeps (the programs design §6.2, §6.3): a member's task that
// comes after another member's gets dep-ready in the program's inbox
// and its own once the last one is met (done, or merged with
// dependency_met merged), once; a dropped one raises dep-gone once.
// While programs are off nothing is looked at, and switching on raises
// what became met meanwhile.
func TestSweepDeps(t *testing.T) {
	r := newRig(t)
	g, err := program.New(program.Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	api, err := project.New(project.Options{Slug: "api"})
	if err != nil {
		t.Fatal(err)
	}
	web := r.p // "demo"
	if _, err := api.Tasks().Add(human, []tasks.NewTask{{Title: "OIDC"}, {Title: "Tokens"}, {Title: "Old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := web.Tasks().Add(human, []tasks.NewTask{{Title: "Login page"}, {Title: "Logout"}}); err != nil {
		t.Fatal(err)
	}
	on := "[features]\nprograms = true\n"
	table := "[programs.acme]\nprojects = [\"demo\", \"api\"]\n"
	cfgPath := filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml")
	write := func(body string) *config.Config {
		t.Helper()
		if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	cfg := write(on + table)
	ws := web.Tasks()
	ws.CrossRefs = program.CrossRefs(cfg, "demo")
	if ws.CrossRefs == nil {
		t.Fatal("no cross refs while on")
	}
	if _, err := ws.SetPlan(human, 1, nil, []string{"api/T1, api/T2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.SetPlan(human, 2, nil, []string{"api/T3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.SetPlan(human, 2, nil, []string{"nope/T3"}); err == nil {
		t.Fatal("a non-member's ref written")
	}
	r.tk.sweepDeps(cfg)
	deps, err := program.ReadDeps("acme")
	if err != nil || deps["api/T1"].State != program.DepOpen || deps["api/T1"].Word() != "open" {
		t.Fatalf("deps %+v %v", deps, err)
	}
	if n := len(depItems(t, &g.Folder, program.KindDepReady)); n != 0 {
		t.Fatalf("first sight raised %d", n)
	}
	// api/T3 dropped: dep-gone, once, in both.
	if _, err := api.Tasks().SetApproved(human, 3, tasks.Dropped, "not needed"); err != nil {
		t.Fatal(err)
	}
	r.tk.sweepDeps(cfg)
	r.tk.sweepDeps(cfg)
	gone := depItems(t, &g.Folder, program.KindDepGone)
	if len(gone) != 1 || gone[0].Subject != "demo/T2" || !strings.Contains(gone[0].Summary, "api/T3, which is dropped") || !gone[0].NeedsUser {
		t.Fatalf("program dep-gone %+v", gone)
	}
	if c := depItems(t, &web.Folder, program.KindDepGone); len(c) != 1 || c[0].Subject != "T2" {
		t.Fatalf("child dep-gone %+v", c)
	}
	// api/T1 done, api/T2's thread merged: demo/T1 can start.
	if _, err := api.Tasks().SetApproved(human, 1, tasks.Done, ""); err != nil {
		t.Fatal(err)
	}
	r.tk.sweepDeps(cfg)
	if n := len(depItems(t, &g.Folder, program.KindDepReady)); n != 0 {
		t.Fatalf("raised with api/T2 open: %d", n)
	}
	if _, err := api.Tasks().SetThread(human, 2, "t-0009"); err != nil {
		t.Fatal(err)
	}
	r.tk.st.Threads["api/t-0009"] = &threadMemo{PR: PR{State: "MERGED"}}
	// Off: nothing happens.
	off := write(table)
	r.tk.sweepDeps(off)
	if n := len(depItems(t, &g.Folder, program.KindDepReady)); n != 0 {
		t.Fatalf("raised while off: %d", n)
	}
	if program.CrossRefs(off, "demo") != nil || program.NoCrossRefs(off, "demo").(*tasks.Error).Code != config.ProgramsOff {
		t.Fatal("cross refs writable while off")
	}
	// On again: what became met is raised.
	cfg = write(on + table)
	r.tk.sweepDeps(cfg)
	r.tk.sweepDeps(cfg)
	ready := depItems(t, &g.Folder, program.KindDepReady)
	if len(ready) != 1 || ready[0].Subject != "demo/T1" || !strings.Contains(ready[0].Summary, "api/T1 done, api/T2 merged") {
		t.Fatalf("program dep-ready %+v", ready)
	}
	if c := depItems(t, &web.Folder, program.KindDepReady); len(c) != 1 || !strings.Contains(c[0].Summary, "api/T1 done, api/T2 merged: T1 can start") {
		t.Fatalf("child dep-ready %+v", c)
	}
	deps, _ = program.ReadDeps("acme")
	if w := program.AfterText(mustTask(t, web, 1), deps); w != "api/T1 (done), api/T2 (merged)" {
		t.Fatalf("after text %q", w)
	}
	// dependency_met done: a merge alone isn't met.
	cfg = write(on + table + "dependency_met = \"done\"\n")
	d := program.NewDeps(cfg, "acme", func(slug, id string) bool { return slug == "api" && id == "t-0009" })
	if s := d.State("api/T2"); s.State != program.DepOpen || !s.Merged {
		t.Fatalf("api/T2 under done: %+v", s)
	}
	for ref, want := range map[string]string{"api/T9": program.DepGone, "other/T1": program.DepUnknown, "mini/api/T1": program.DepUnknown} {
		if s := d.State(ref); s.State != want {
			t.Errorf("%s: %+v, want %s", ref, s, want)
		}
	}
}

func mustTask(t *testing.T, p *project.Project, id int) *tasks.Task {
	t.Helper()
	tk, err := p.Tasks().Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// TestSweepUmbrellas (the programs design §5.3): the sweep writes an
// umbrella's status from its parts (started while any part moves,
// blocked while one is, review once all are met, with an
// umbrella-review item for the user, once), its parts' states and the
// members' waits to deps.json; done is the user's and stays.
func TestSweepUmbrellas(t *testing.T) {
	r := newRig(t)
	g, err := program.New(program.Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	api, err := project.New(project.Options{Slug: "api"})
	if err != nil {
		t.Fatal(err)
	}
	web := r.p // "demo"
	if _, err := api.Tasks().Add(human, []tasks.NewTask{{Title: "OIDC"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := web.Tasks().Add(human, []tasks.NewTask{{Title: "Login page"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte("[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\", \"api\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ws := web.Tasks()
	ws.CrossRefs = program.CrossRefs(cfg, "demo")
	if _, err := ws.SetPlan(human, 1, nil, []string{"api/T1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Tasks().Add(human, []tasks.NewTask{{Title: "SSO"}}); err != nil {
		t.Fatal(err)
	}
	// As an older route noted them: the sweep adopts them as parts.
	if _, err := g.Tasks().ProgramEdit(human, "acme", 1, tasks.ProgramChange{Note: "routed api/T1, demo/T1, mini/ios (pending M-1)"}); err != nil {
		t.Fatal(err)
	}
	umbrella := func() tasks.Status {
		t.Helper()
		u, err := g.Tasks().Get(1)
		if err != nil {
			t.Fatal(err)
		}
		return u.Status
	}
	r.tk.sweepDeps(cfg)
	if u, _ := g.Tasks().Get(1); strings.Join(u.Parts, " ") != "api/T1 demo/T1" {
		t.Fatalf("adopted parts %v", u.Parts)
	}
	if st := umbrella(); st != tasks.Open {
		t.Fatalf("all waiting: %s", st)
	}
	snap, err := program.ReadSnapshot("acme")
	if err != nil || len(snap.Waits) != 1 || snap.Waits[0] != (program.Wait{Task: "demo/T1", Title: "Login page", Status: "open", After: "api/T1"}) {
		t.Fatalf("waits %+v %v", snap.Waits, err)
	}
	if snap.States["demo/T1"].State != program.DepOpen || snap.States["api/T1"].Status != "open" {
		t.Fatalf("states %+v", snap.States)
	}
	if _, err := api.Tasks().SetStatus(human, 1, tasks.Started, ""); err != nil {
		t.Fatal(err)
	}
	r.tk.sweepDeps(cfg)
	if st := umbrella(); st != tasks.Started {
		t.Fatalf("one started: %s", st)
	}
	if _, err := web.Tasks().SetStatus(human, 1, tasks.Blocked, ""); err != nil {
		t.Fatal(err)
	}
	r.tk.sweepDeps(cfg)
	if st := umbrella(); st != tasks.Blocked {
		t.Fatalf("one blocked: %s", st)
	}
	if !strings.Contains(journalText(t, &g.Folder), "ticker task.status T1 blocked (from its parts)") {
		t.Fatalf("journal:\n%s", journalText(t, &g.Folder))
	}
	for _, p := range []*project.Project{api, web} {
		if _, err := p.Tasks().SetApproved(human, 1, tasks.Done, ""); err != nil {
			t.Fatal(err)
		}
	}
	r.tk.sweepDeps(cfg)
	r.tk.sweepDeps(cfg)
	if st := umbrella(); st != tasks.Review {
		t.Fatalf("all done: %s", st)
	}
	items := depItems(t, &g.Folder, program.KindUmbrellaReview)
	if len(items) != 1 || items[0].Subject != "T1" || !items[0].NeedsUser || !strings.Contains(items[0].Summary, "every part of acme/T1 is in (api/T1, demo/T1)") {
		t.Fatalf("items %+v", items)
	}
	if _, err := g.Tasks().SetStatus(human, 1, tasks.Done, ""); err != nil {
		t.Fatal(err)
	}
	r.tk.sweepDeps(cfg)
	if st := umbrella(); st != tasks.Done {
		t.Fatalf("done by the user: %s", st)
	}
}

func journalText(t *testing.T, f *project.Folder) string {
	t.Helper()
	lines, _, err := f.JournalTail(50)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
