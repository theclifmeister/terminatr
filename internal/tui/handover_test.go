package tui

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestSwitchFrame: a switch to another view (laid out anew: c.full) is
// drawn in one synchronised update that overwrites every cell in place,
// the screen never cleared first: whatever the window showed (the
// dashboard's last frame, here junk) stays up until it is replaced, even
// on a terminal that shows an update before its end (T372).
func TestSwitchFrame(t *testing.T) {
	c := infoClient(t, 200)
	mirror, err := emu.New(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	mirror.Write([]byte("hello from the pane"))
	r, err := emu.NewRenderer(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	c.panes["s-4"] = &pane{mirror: mirror, r: r, info: proto.SessionInfo{ID: "s-4"}}
	t.Cleanup(func() {
		delete(c.panes, "s-4") // no connection to close
		r.Close()
		mirror.Close()
	})
	c.relayout()
	if !c.full {
		t.Fatal("a new layout doesn't repaint")
	}
	b, err := c.frameSplit(c.shown())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte(syncStart)) || !bytes.HasSuffix(b, []byte(syncEnd)) ||
		bytes.Count(b, []byte(syncStart)) != 1 || bytes.Count(b, []byte(syncEnd)) != 1 {
		t.Errorf("not one synchronised update: %q", b)
	}
	for _, clear := range []string{"\x1b[2J", "\x1b[J", "\x1b[0J", "\x1b[1J", "\x1b[3J"} {
		if bytes.Contains(b, []byte(clear)) {
			t.Errorf("the switch clears the screen (%q): %q", clear, b)
		}
	}

	outer, err := emu.New(uint16(c.cols), uint16(c.rows))
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	junk := make([]string, c.rows)
	for i := range junk {
		junk[i] = strings.Repeat("¤", c.cols)
	}
	outer.Write([]byte("\x1b[H" + strings.Join(junk, "\r\n")))
	outer.Write(b)
	screen, err := outer.Screen()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(screen, "¤") || !strings.Contains(screen, "hello from the pane") {
		t.Errorf("the frame doesn't cover the window:\n%s", screen)
	}
	if b, _ := c.frameSplit(c.shown()); b != nil {
		t.Errorf("a second frame: %q", b)
	}
}

// dashOutFile is a dashOut writing to a file, and a func reading what it
// wrote so far.
func dashOutFile(t *testing.T, handover bool) (*dashOut, func() string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return newDashOut(f, handover), func() string {
		b, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}

// TestDashOutHandover: handed the screen, the dashboard doesn't enter the
// alternate screen again, and holds what it draws until it has its data:
// the frame that draws that carries the rest, one synchronised update.
// Leaving, it keeps the screen up, the cursor hidden (T372).
func TestDashOutHandover(t *testing.T) {
	o, written := dashOutFile(t, true)
	o.Write([]byte("\x1b[?2026$p")) // Bubble Tea's queries
	o.Write([]byte(enterScreen + "\x1b[?25l\x1b[H\x1b[2J loading…"))
	if got := written(); got != "" {
		t.Fatalf("wrote while loading: %q", got)
	}
	o.loaded()
	o.Write([]byte("\x1b[?2026h\x1b[1;1H the list\x1b[?2026l"))
	want := syncStart + "\x1b[?2026$p\x1b[?25l\x1b[H\x1b[2J loading…\x1b[1;1H the list" + syncEnd
	if got := written(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	o.Write([]byte("\x1b[>4m\r" + leaveAlt + showCur + "\x1b[?2004l"))
	if got := strings.TrimPrefix(written(), want); got != syncStart+"\x1b[>4m\r\x1b[?2004l"+syncEnd {
		t.Errorf("leaving: %q", got)
	}
}

// TestDashOutHold: a dashboard whose data doesn't come draws what it has
// after holdMax; without a handover it holds nothing, enters the
// alternate screen and synchronises every write.
func TestDashOutHold(t *testing.T) {
	o, written := dashOutFile(t, true)
	o.Write([]byte(enterScreen + " loading…"))
	deadline := time.Now().Add(5 * holdMax)
	for written() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := written(); got != syncStart+" loading…"+syncEnd {
		t.Errorf("after holdMax: %q", got)
	}

	o, written = dashOutFile(t, false)
	o.Write([]byte(enterScreen + "\x1b[H\x1b[2J frame"))
	if got := written(); got != syncStart+enterScreen+"\x1b[H\x1b[2J frame"+syncEnd {
		t.Errorf("no handover: %q", got)
	}
}
