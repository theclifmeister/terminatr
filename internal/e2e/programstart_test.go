package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProgramStart (B12; the programs design §4.4, §10, B11): a
// delegate program's director starts a thread in a member with tm
// program start, through the server: the member's thread, its task
// started and linked, the start journaled in the member and the program,
// and the slot held for the director ended. With the program's one slot
// taken by a working thread, its next start is refused over-program-cap,
// tells it never to pass --over-cap, and the member's coordinator waits
// for the slot.
func TestProgramStart(t *testing.T) {
	env, projDir, out := tickerEnv(t)
	other, otherDir := newProject(env, "other")
	env.MustCLI("task", "add", "Ship infra", "--project", "demo")
	env.MustCLI("task", "add", "Web part", "--project", other)
	tm := `"$TERMINATR_BIN" `
	for name, body := range map[string]string{
		"pstart": `
[[step]]
do = "run"
cmd = '` + tm + `program start demo/T1; echo "start""=$?"'
`,
		"pstart2": `
[[step]]
do = "run"
cmd = '` + tm + `program start other/T1 > "$OUT/second" 2>&1; echo "second""=$?"; echo PSTART"-"DONE'
`,
	} {
		if err := os.WriteFile(filepath.Join(env.scriptsDir(), name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig(t, env, "[features]\nprograms = true\n[defaults]\nstart_threads = \"auto\"\n[programs.studio]\nprojects = [\"demo\", \"other\"]\nauthority = \"delegate\"\nparallel_threads = 1\n")
	var g struct{ Slug, Dir string }
	if err := json.Unmarshal([]byte(env.MustCLI("program", "new", "studio", "--json")), &g); err != nil {
		t.Fatal(err)
	}
	env.Trust(g.Dir, otherDir)

	// A slot the ticker held for the director (B11).
	slots := filepath.Join(env.Home, "state", "slots.json")
	os.MkdirAll(filepath.Dir(slots), 0o700)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	hold := `[{"project":"","director":"studio","code":"slot-free","at":"` + now + `","last":"` + now + `","told":"` + now + `"}]` + "\n"
	if err := os.WriteFile(slots, []byte(hold), 0o600); err != nil {
		t.Fatal(err)
	}

	d := &Session{ID: strings.TrimSpace(env.MustCLI("program", "open", "studio"))}
	info, _ := env.Info(d)
	env.track(info.PID, "director "+d.ID)
	env.WaitState(d, "idle", agentWait)
	env.Prompt(d, "run pstart")
	env.WaitFor(d, "start=", agentWait) // the output: the command says start""=
	if sc := env.Screen(d); !strings.Contains(sc, "start=0") {
		t.Fatalf("the director's start:\n%s", sc)
	}
	var rec struct{ Session, Task string }
	readTOML(t, filepath.Join(projDir, "threads", "t-0001", "thread.toml"), &rec)
	if rec.Task != "T1" {
		t.Fatalf("thread record %+v", rec)
	}
	if show := env.MustCLI("task", "show", "T1", "--project", "demo"); !strings.Contains(show, "started") || !strings.Contains(show, "t-0001") {
		t.Fatalf("demo's T1:\n%s", show)
	}
	j, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	for _, w := range []string{"director:studio thread.start t-0001 T1 Ship infra", "director:studio task.status T1 started"} {
		if !strings.Contains(string(j), w) {
			t.Errorf("demo's journal lacks %q:\n%s", w, j)
		}
	}
	if pj, _ := os.ReadFile(filepath.Join(g.Dir, "JOURNAL.md")); !strings.Contains(string(pj), "director:studio program.start demo/T1 t-0001 Ship infra") {
		t.Errorf("the program's journal:\n%s", pj)
	}
	if b, _ := os.ReadFile(slots); strings.Contains(string(b), `"director"`) {
		t.Fatalf("the director's hold outlived its start:\n%s", b)
	}

	// The thread works: the program's one slot is taken.
	th := &Session{ID: rec.Session}
	env.WaitState(th, "idle", agentWait)
	env.MustCLI("thread", "prompt", "t-0001", "run thread-block", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	env.Prompt(d, "run pstart2")
	env.WaitFor(d, "PSTART-DONE", agentWait)
	if sc := env.Screen(d); !strings.Contains(sc, "second=1") {
		t.Fatalf("the director's second start:\n%s", sc)
	}
	second, _ := os.ReadFile(filepath.Join(out, "second"))
	for _, w := range []string{"over-program-cap", "1 of 1 threads are working in program studio", "never pass --over-cap"} {
		if !strings.Contains(string(second), w) {
			t.Errorf("the refusal lacks %q:\n%s", w, second)
		}
	}
	if _, err := os.Stat(filepath.Join(otherDir, "threads", "t-0001")); !os.IsNotExist(err) {
		t.Fatalf("a refused start left a thread: %v", err)
	}
	if b, _ := os.ReadFile(slots); !strings.Contains(string(b), `"project": "other"`) || !strings.Contains(string(b), "over-program-cap") {
		t.Fatalf("other's coordinator doesn't wait for the slot:\n%s", b)
	}
}
