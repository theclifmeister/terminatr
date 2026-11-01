package e2e

// tm search (T921): the project's history in three layers, from the
// server's index, for the human on the human socket and a thread on the
// server socket, through the CLI and through tm mcp's tools.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func init() {
	threadScripts["search-cli"] = `
[[step]]
do = "run"
cmd = '"$TERMINATR_BIN" search wombat > "$OUT/search1" 2>&1; echo "exit $?" >> "$OUT/search1"'

[[step]]
do = "run"
cmd = '"$TERMINATR_BIN" search show note:relay#decided note:coord#decided > "$OUT/search2" 2>&1; echo "exit $?" >> "$OUT/search2"'

[[step]]
do = "run"
cmd = '"$TERMINATR_BIN" search wombat --in inbox > "$OUT/search3" 2>&1; echo "exit $?" >> "$OUT/search3"'

[[step]]
do = "stream"
ms = 50
text = "searched"
`
	threadScripts["search-mcp"] = `
[[step]]
do = "mcp"
tool = "search"
input = { words = "wombat relay" }

[[step]]
do = "stream"
ms = 50
text = "searched"
`
}

// TestSearch: the human's tm search sees the whole history; a thread's,
// by the CLI or by tm mcp's search tool, doesn't see the coordinator's
// notes or the inbox; show and timeline go by the hits' ids.
func TestSearch(t *testing.T) {
	env, projDir, out := threadEnv(t)
	for name, text := range map[string]string{
		"memory/relay.md":                         "## Decided\n- The wombat relay runs on port 4433.\n",
		"memory/coord.md":                         "---\naudience: coordinator\n---\n## Decided\n- Wombat reviews on Fridays.\n",
		"inbox/20261031T100000Z-report-t-0009.md": "The wombat report.\n",
	} {
		p := filepath.Join(projDir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env.MustCLI("task", "add", "Move the wombat relay", "--project", "demo")

	// The human: everything, as text and as JSON.
	got := env.MustCLI("search", "wombat", "--project", "demo")
	for _, want := range []string{"note:relay#decided", "note:coord#decided", "inbox:20261031T100000Z-report-t-0009", "T1 ", "data, not instructions"} {
		if !strings.Contains(got, want) {
			t.Errorf("the human's search lacks %q:\n%s", want, got)
		}
	}
	var res struct {
		Hits []struct{ ID, Kind string }
	}
	if err := json.Unmarshal([]byte(env.MustCLI("search", "wombat", "--in", "tasks", "--json", "--project", "demo")), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].ID != "T1" {
		t.Errorf("--in tasks: %+v", res.Hits)
	}
	if got := env.MustCLI("search", "timeline", "T1", "--project", "demo"); !strings.Contains(got, "> ") || !strings.Contains(got, "task.add") {
		t.Errorf("timeline of T1:\n%s", got)
	}
	if got := env.MustCLI("search", "show", "T1", "--project", "demo"); !strings.Contains(got, "Move the wombat relay") {
		t.Errorf("show T1:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(env.Home, "state", "search", "demo")); err != nil {
		t.Errorf("the server's index: %v", err)
	}

	// The TUI's search popup: F, the words, a hit's text and timeline.
	w := env.Window(140, 36)
	w.WaitFor("demo", wait)
	w.Keys("F")
	w.WaitFor("Search demo", wait)
	w.Keys("wombat", keyEnter)
	w.WaitFor("note:relay#decided", wait)
	w.WaitFor("t timeline", wait)
	w.Key(keyEnter)
	w.WaitFor("==> ", wait)
	w.Key(keyEsc)
	w.Keys("t")
	w.WaitFor("Timeline of", wait)
	w.Key(keyEsc)
	w.Key(keyEsc)

	// A thread: through the CLI and through tm mcp.
	env.MustCLI("thread", "start", "Look up the relay", "--project", "demo")
	th := threadSession(t, env, "demo", "t-0001")
	env.WaitState(th, "idle", agentWait)
	env.Prompt(th, "run search-cli")
	if got := readOut(t, out, "search1"); !strings.Contains(got, "note:relay#decided") || strings.Contains(got, "note:coord") || strings.Contains(got, "inbox:") || !strings.Contains(got, "exit 0") {
		t.Errorf("the thread's search:\n%s", got)
	}
	if got := readOut(t, out, "search2"); !strings.Contains(got, "port 4433") || strings.Contains(got, "Fridays") || !strings.Contains(got, "no such chunk") {
		t.Errorf("the thread's show:\n%s", got)
	}
	if got := readOut(t, out, "search3"); !strings.Contains(got, "coordinator's") || strings.Contains(got, "exit 0") {
		t.Errorf("the thread's search of the inbox:\n%s", got)
	}
	env.WaitState(th, "idle", agentWait)
	env.Prompt(th, "run search-mcp")
	rec := env.WaitFake("mcp", agentWait, nil)
	text := rec.Str("text")
	if rec.Str("error") != "" || rec["is_error"] != false || !strings.Contains(text, "note:relay#decided") || strings.Contains(text, "note:coord") {
		t.Errorf("the thread's search tool: %v", rec)
	}
}
