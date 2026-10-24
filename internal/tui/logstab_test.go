package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/logfile"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func (f *fakeSource) Logs() ([]proto.LogInfo, error) {
	out := append([]proto.LogInfo(nil), f.logs...)
	for i := range out {
		fi, err := os.Stat(out[i].Path)
		out[i].Missing = err != nil
		if err == nil {
			out[i].Size = fi.Size()
		}
	}
	return out, nil
}

// ReadLog reads as log.read does, by id.
func (f *fakeSource) ReadLog(p proto.LogReadParams) (proto.LogReadResult, error) {
	f.logReads = append(f.logReads, p)
	for _, l := range f.logs {
		if l.ID != p.ID {
			continue
		}
		var c logfile.Chunk
		var err error
		if p.After != nil {
			c, err = logfile.After(l.Path, *p.After, p.Max)
		} else {
			c, err = logfile.Tail(l.Path, p.Before, p.Max)
		}
		return proto.LogReadResult{Text: string(c.Text), Start: c.Start, End: c.End, Size: c.Size, Reset: c.Reset}, err
	}
	return proto.LogReadResult{}, errors.New("no log " + p.ID)
}

// logsData is a dashboard whose server has a long server log, a console
// log whose lines carry levels, and a mod's log.
func logsData(t *testing.T) (*fakeSource, *dash, string) {
	t.Helper()
	src, m := popupData(t)
	dir := t.TempDir()
	var b strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&b, "2026/10/18 01:%02d:%02d.000000 session s-%d: claude working\n", i/60%60, i%60, i)
	}
	server := filepath.Join(dir, "server.log")
	os.WriteFile(server, []byte(b.String()), 0o600)
	console := filepath.Join(dir, "console.log")
	os.WriteFile(console, []byte("pid 1: 2026/10/18 01:00:00 error: the settings can't be read\npid 1: 2026/10/18 01:00:01 dashboard ok\npid 2: 2026/10/18 01:00:02 warning: slow view\n"), 0o600)
	src.logs = []proto.LogInfo{
		{ID: proto.LogServer, Name: "Server", Path: server},
		{ID: proto.LogConsole, Name: "Console", Path: console},
		{ID: "mod:linear", Name: "mod linear", Path: filepath.Join(dir, "mods", "linear.log")},
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	press(m, ",", "5")
	screen(m) // the first drawing lists the logs
	return src, m, server
}

// TestLogsTab: the , popup's sixth tab shows the server's log's end,
// read in one chunk, follows it, scrolls up a chunk at a time, searches,
// and picks another log.
func TestLogsTab(t *testing.T) {
	src, m, server := logsData(t)
	out := screen(m)
	for _, want := range []string{"5 logs", " Server ", " Console ", " mod linear ", "server.log", "following", "s-4999: claude working", "more ↑"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if len(src.logReads) != 1 || src.logReads[0].Max != logChunk || src.logReads[0].After != nil {
		t.Fatalf("reads %+v", src.logReads)
	}
	lt := &m.top().(*settingsView).logs
	if lt.start == 0 || lt.end != lt.size {
		t.Fatalf("not a tail chunk: %d-%d of %d", lt.start, lt.end, lt.size)
	}
	held := len(lt.lines)

	// Following: the tick reads what was added after the end.
	f, _ := os.OpenFile(server, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("2026/10/18 02:00:00.000000 session s-9999: claude idle\n")
	f.Close()
	drain(t, m, m.logsTick(), reflect.TypeOf(logMsg{}))
	if last := src.logReads[len(src.logReads)-1]; last.After == nil || *last.After == 0 {
		t.Errorf("follow read %+v", last)
	}
	if out := screen(m); !strings.Contains(out, "s-9999: claude idle") {
		t.Errorf("didn't follow:\n%s", out)
	}

	// Scrolling up past what it holds reads the chunk before, and pauses.
	for range held + 5 {
		keyPress(m, "up")
	}
	if lt.follow || len(lt.lines) <= held+1 {
		t.Errorf("scrolled up: follow %v, %d lines (held %d)", lt.follow, len(lt.lines), held)
	}
	if last := src.logReads[len(src.logReads)-1]; last.Before == 0 || last.After != nil {
		t.Errorf("older read %+v", last)
	}
	if out := screen(m); !strings.Contains(out, "paused (f follows)") {
		t.Errorf("not paused:\n%s", out)
	}
	if cmd := m.logsTick(); cmd != nil {
		t.Error("polls while paused")
	}

	// / searches what it holds, older chunks when need be.
	press(m, "/", "s", "-", "1", "2", "3", ":")
	if !lt.typing || !strings.Contains(screen(m), "/s-123:▏") {
		t.Fatalf("search line:\n%s", screen(m))
	}
	keyPress(m, "enter")
	if lt.hit < 0 || !strings.Contains(lt.lines[lt.hit].text, "s-123:") {
		t.Fatalf("no hit: %d %q", lt.hit, lt.note)
	}
	if lt.start != 0 {
		// 5000 lines are ~290 KB: s-123 is in the file's first chunk.
		t.Errorf("didn't read back to the match: start %d", lt.start)
	}
	press(m, "n")
	if !strings.Contains(lt.note, "no older match") {
		t.Errorf("n: %q", lt.note)
	}
	// G follows again.
	press(m, "G")
	if !lt.follow || !strings.Contains(screen(m), "s-9999") {
		t.Error("G doesn't follow")
	}

	// ] picks the console's log; its lines carry levels, which v keeps.
	press(m, "]")
	out = screen(m)
	if !strings.Contains(out, "console.log") || !strings.Contains(out, "slow view") || !strings.Contains(out, "dashboard ok") {
		t.Fatalf("console log:\n%s", out)
	}
	press(m, "v")
	if out := screen(m); !strings.Contains(out, "warnings and errors") || strings.Contains(out, "dashboard ok") || !strings.Contains(out, "slow view") {
		t.Errorf("v warn:\n%s", out)
	}
	press(m, "v")
	if out := screen(m); !strings.Contains(out, "errors only") || strings.Contains(out, "slow view") || !strings.Contains(out, "can't be read") {
		t.Errorf("v error:\n%s", out)
	}
	// A mod's log with no file yet.
	press(m, "]")
	if out := screen(m); !strings.Contains(out, "No file yet") {
		t.Errorf("missing log:\n%s", out)
	}
	// h and l stay the tabs'; [ goes back round.
	press(m, "[", "[", "[")
	if lt.id() != "mod:linear" {
		t.Errorf("[ round: %s", lt.id())
	}
	press(m, "h")
	if m.top().(*settingsView).tab != tabMods {
		t.Error("h didn't leave the tab")
	}
}

// TestLogsRotated: a log shorter than the end it followed was rotated:
// the tab starts it anew.
func TestLogsRotated(t *testing.T) {
	_, m, server := logsData(t)
	lt := &m.top().(*settingsView).logs
	os.WriteFile(server, []byte("2026/10/18 03:00:00 server started anew\n"), 0o600)
	drain(t, m, m.logsTick(), reflect.TypeOf(logMsg{}))
	out := screen(m)
	if !strings.Contains(out, "started anew") || strings.Contains(out, "s-4999") || !strings.Contains(out, "rotated") || len(lt.lines) != 1 {
		t.Errorf("rotation:\n%s", out)
	}
}

// TestLogsLevel: what counts as a line's level.
func TestLogsLevel(t *testing.T) {
	for line, want := range map[string]string{
		"pid 1: 2026/10/18 01:00:00 error: x":          "error",
		"time=1 level=WARN msg=x":                      "warn",
		"[info] started":                               "info",
		"2026/10/18 ERROR boom":                        "error",
		"2026/10/18 session s-1: claude working":       "",
		"2026/10/18 errors in the summary don't count": "",
		"attach: panic: nil map":                       "error",
	} {
		if got := lineLevel(line); got != want {
			t.Errorf("%q: %q, want %q", line, got, want)
		}
	}
	// What a log holds is shown safe: no escape reaches the terminal.
	if l := splitLog("a\x1b]52;c;aGk=\x07b\tc\n"); len(l) != 1 || strings.ContainsAny(l[0].text, "\x1b\x07\t") {
		t.Errorf("unsafe: %q", l)
	}
}

// TestLogsPager: o opens only a regular file on this host by its
// absolute path, which no pager takes for an option.
func TestLogsPager(t *testing.T) {
	_, m, _ := logsData(t)
	lt := &m.top().(*settingsView).logs
	for _, path := range []string{"+!touch pwned", "-o/tmp/x", filepath.Join(t.TempDir(), "none.log")} {
		lt.logs[lt.cur].Path, lt.note = path, ""
		if cmd := lt.pager(m); cmd != nil || !strings.Contains(lt.note, "isn't on this host") {
			t.Errorf("%q: %v %q", path, cmd != nil, lt.note)
		}
	}
}
