package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// logPathMask hides the log's path in the Logs tab's head, its start cut.
var logPathMask = Mask{Name: "path", Re: regexp.MustCompile(`(…|[/\\])\S*[/\\]logs[/\\]\S+ *`), Repl: "<path> "}

// TestSmokeLogsTab: the , popup's sixth tab lists the server host's
// logs, read through the server: the server's (following it), the
// consoles' and a mod's; ] picks the mod's, and / finds a line in it.
func TestSmokeLogsTab(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	// The built-in code mods off, from before the server starts: none
	// writes a log, so the picker lists the server's, the consoles' and
	// demo's alone.
	off := ""
	for _, id := range []string{"memory-tab", "pr-ci-column", "tasks-tab", "ticker"} {
		off += "[mods." + id + "]\nenabled = false\n"
	}
	if err := os.WriteFile(filepath.Join(env.Home, "config.toml"), []byte(off), 0o600); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(env.Home, "logs", "mods")
	if err := os.MkdirAll(mods, 0o700); err != nil {
		t.Fatal(err)
	}
	log := "2026-10-18T09:00:00.000+02:00 supervisor: started\n" +
		"2026-10-18T09:00:01.000+02:00 broker: ui.view.open board\n" +
		"2026-10-18T09:00:02.000+02:00 supervisor: stopped\n"
	if err := os.WriteFile(filepath.Join(mods, "demo.log"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}

	w := env.Window(110, 30)
	w.WaitFor("+ new project", wait)
	w.Keys(",", "6")
	w.WaitFor("server.log", wait)
	w.WaitFor("following", wait)
	w.Keys("]", "]")
	w.WaitFor("ui.view.open board", wait)
	w.Golden("logs-mod.txt", append([]Mask{logPathMask}, popupMasks...)...)
	w.Keys("/", "board", keyEnter)
	w.WaitFor("/board", wait)
	w.WaitFor("paused (f follows)", wait)
	w.Key(keyEsc)
}
