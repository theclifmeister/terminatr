package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDirectorProgramQuestions: two member coordinators' alike
// questions are flagged similar in the program's Needs you; the
// director's /answer says there is nothing to answer until they are
// merged into a program question (tm program ask merge, here the
// human's), then opens it on the director's session. Attached to the
// director, the user picks an option in the menu over the pane, and
// each member question gets the answer in its own words, as the user's
// answer there; the program journals the fan-out.
func TestDirectorProgramQuestions(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	api, apiDir := newProject(env, "api")
	web, webDir := newProject(env, "web")
	writeConfig(t, env, "[features]\nprograms = true\n[programs.studio]\nprojects = [\""+api+"\", \""+web+"\"]\n")
	env.MustCLI("program", "new", "studio")
	progDir := filepath.Join(env.Home, "projects", "_programs", "studio")
	env.Trust(progDir, apiDir, webDir)
	questions := func(dir, text, a, b string) {
		t.Helper()
		body := `{"next_id": 2, "open": [{"id": "Q1", "header": "Region", "question": "` + text + `",
  "options": [{"label": "` + a + `"}, {"label": "` + b + `"}], "asked": "2026-11-01T10:00:00Z"}]}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "questions.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	questions(apiDir, "Which region do we deploy to?", "EU", "US")
	questions(webDir, "Which region should we deploy to?", "us", "eu")

	id := strings.TrimSpace(env.MustCLI("program", "open", "studio"))
	d := &Session{ID: id}
	info, _ := env.Info(d)
	d.PID = info.PID
	env.track(info.PID, "director "+id)
	env.WaitState(d, "idle", agentWait)
	if needs := env.MustCLI("program", "needs", "studio", "--json"); !strings.Contains(needs, `"similar": [`) || !strings.Contains(needs, `"Q1@`+web+`"`) {
		t.Fatalf("no similarity hint:\n%s", needs)
	}
	blocked := func(prompt string) string {
		t.Helper()
		n := len(env.FakeRecords("blocked"))
		env.Prompt(d, prompt)
		var r FakeRecord
		if !Poll(agentWait, func() bool {
			if b := env.FakeRecords("blocked"); len(b) > n {
				r = b[n]
			}
			return r != nil
		}) {
			t.Fatalf("%s wasn't blocked; prompts %v", prompt, env.FakeRecords("prompt"))
		}
		env.WaitState(d, "idle", agentWait)
		return r.Str("reason")
	}
	if got := blocked("/answer"); !strings.HasPrefix(got, "Not asked: program studio has no open questions") {
		t.Fatalf("/answer with none merged: %q", got)
	}

	if out := env.MustCLI("program", "ask", "merge", "Q1@"+api, web+"/Q1", "--program", "studio"); !strings.Contains(out, "merged into studio/Q1") {
		t.Fatalf("merge: %s", out)
	}
	if out := env.MustCLI("ask", "list", "--project", web); !strings.Contains(out, "covered by studio/Q1") {
		t.Fatalf("web's question isn't marked covered:\n%s", out)
	}
	if got := blocked("/answer"); !strings.HasPrefix(got, "Asked: the program's 1 question(s) are open") {
		t.Fatalf("/answer: %q", got)
	}

	w := env.Attach(100, 40, id)
	w.WaitFor("Which region do we deploy to?", wait)
	w.WaitFor("Your answer goes to every member question it covers.", wait)
	w.Type("1")
	for slug, want := range map[string]string{api: `the user answers Q1: "EU"`, web: `the user answers Q1: "eu"`} {
		if !Poll(wait, func() bool { return strings.Contains(env.MustCLI("inbox", "list", "--project", slug), want) }) {
			t.Errorf("%s: no answer item %q:\n%s", slug, want, env.MustCLI("inbox", "list", "--project", slug))
		}
	}
	j, err := os.ReadFile(filepath.Join(progDir, "JOURNAL.md"))
	if err != nil || !strings.Contains(string(j), "human (program studio) program.answer Q1") {
		t.Errorf("program journal: %v\n%s", err, j)
	}
	if out := env.MustCLI("program", "ask", "list", "--program", "studio"); strings.TrimSpace(out) != "" {
		t.Errorf("the program question is still open:\n%s", out)
	}
	w.Detach()
	w.WaitExit(wait)
}
