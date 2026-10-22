package session

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/term"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

// boxAgentEnv makes the test binary boxAgent.
const boxAgentEnv = "TM_TEST_BOX_AGENT"

func init() {
	if os.Getenv(boxAgentEnv) != "" {
		boxAgent(os.Getenv(boxAgentEnv))
		os.Exit(0)
	}
}

// boxAgent draws Claude's prompt box, as an agent does: the text typed
// and pasted into it, or the dim suggestion while it is empty; Enter
// sends it, and ? on an empty box shows the shortcuts below it instead.
// Other keys, and the mouse it asks for, change nothing.
func boxAgent(suggestion string) {
	if st, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), st)
	}
	var line []byte
	help := false
	draw := func() {
		box := string(line)
		if box == "" {
			box = "\x1b[2m" + suggestion + "\x1b[22m"
		}
		fmt.Printf("\x1b[H\x1b[2J\x1b[?1000h\x1b[?1006h\r\n───\r\n❯ %s\r\n───\r\n", box)
		if help {
			fmt.Print("  ? for shortcuts · esc to interrupt\r\n")
		}
	}
	draw()
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for b := buf[:n]; len(b) > 0; {
			kind, k := emu.NextInput(b)
			switch {
			case kind == emu.InputText && b[0] == '?' && len(line) == 0:
				help = !help
			case kind == emu.InputText:
				line = append(line, b[:k]...)
			case kind == emu.InputPaste:
				line = append(line, bytes.TrimSuffix(bytes.TrimPrefix(b[:k], []byte("\x1b[200~")), []byte("\x1b[201~"))...)
			case b[0] == '\r':
				line = line[:0]
			}
			b = b[k:]
		}
		draw()
	}
}

// startBoxAgent runs boxAgent under the claude manifest, which reads
// its box.
func startBoxAgent(t *testing.T, suggestion string) *Session {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(Config{
		ID: "s-test", Role: proto.RoleThread, Project: "p",
		Argv: []string{exe, "-test.run=^$"},
		Cwd:  t.TempDir(),
		Env:  []string{boxAgentEnv + "=" + suggestion, "TERM=xterm-256color", "LANG=C.UTF-8"},
		Cols: 80, Rows: 24,
		Logf: t.Logf,
		Agent: &AgentConfig{Agent: claudeLike(t), Home: t.TempDir(), PromptHold: time.Hour,
			OnPromptResolved: func(*Session, PromptResolution) {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(time.Second) })
	if _, err := s.Hook("SessionStart", map[string]any{"source": "startup"}); err != nil {
		t.Fatal(err)
	}
	testwait.Eventually(t, "the prompt box", func() bool { return strings.Contains(screen(t, s), suggestion) })
	return s
}

// TestHumanInputTakeover (T691): a takeover is a person's text in the
// prompt box. Clicks, the wheel, focus, arrows, page keys, Esc and Tab
// over the agent's dim suggestion take nothing over, nor does a ? that
// shows the shortcuts and leaves the box to the suggestion, nor tm's own
// keys; a typed key or a paste that shows in the box does.
func TestHumanInputTakeover(t *testing.T) {
	defer func(w time.Duration) { boxWatchWait = w }(boxWatchWait)
	boxWatchWait = time.Second
	s := startBoxAgent(t, "try: fix the tests")
	var took atomic.Int32
	count := func() { took.Add(1) }
	for _, in := range []string{
		"\x1b[<0;5;3M\x1b[<0;5;3m", "\x1b[<2;5;3M\x1b[<2;5;3m", // clicks
		"\x1b[<64;5;3M\x1b[<65;5;3M", // the wheel
		"\x1b[I\x1b[O",               // focus
		"\x1b[A\x1b[B\x1b[C\x1b[D", "\x1b[5~\x1b[6~", "\x1b[H\x1b[F",
		"\t", "\x1b",
		"?", // the shortcuts, below the box
	} {
		if err := s.HumanInput([]byte(in), count); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	testwait.Eventually(t, "the shortcuts", func() bool { return strings.Contains(screen(t, s), "? for shortcuts") })
	never(t, boxWatchWait+time.Second, "a takeover", func() bool { return took.Load() > 0 })
	if sc := screen(t, s); !strings.Contains(sc, "try: fix the tests") {
		t.Fatalf("the suggestion went:\n%s", sc)
	}

	// tm's own keys type into the box, as no person.
	s.Input([]byte("from tm"))
	testwait.Eventually(t, "tm's keys", func() bool { return strings.Contains(screen(t, s), "❯ from tm") })
	s.Input([]byte("\r"))
	never(t, time.Second, "a takeover", func() bool { return took.Load() > 0 })

	s.HumanInput([]byte("x"), count)
	testwait.Eventually(t, "a takeover", func() bool { return took.Load() > 0 })
	s.HumanInput([]byte("\r"), nil)

	took.Store(0)
	s.HumanInput([]byte("\x1b[200~pasted\x1b[201~"), count)
	testwait.Eventually(t, "a paste's takeover", func() bool { return took.Load() > 0 })
}

// TestHumanInputNoBox: a program whose box tm doesn't read takes a
// person's text over at once, and no other key.
func TestHumanInputNoBox(t *testing.T) {
	s := start(t, "/bin/sh")
	var took atomic.Int32
	count := func() { took.Add(1) }
	s.HumanInput([]byte("\x1b[A\x1b[<0;1;1M\x1b"), count)
	if took.Load() != 0 {
		t.Fatal("a takeover without text")
	}
	s.HumanInput([]byte("e"), count)
	if took.Load() != 1 {
		t.Fatal("no takeover on text")
	}
}
