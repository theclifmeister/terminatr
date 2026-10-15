package modsup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

// TestModTextLoggedAsOneLine: what a host sends the log stays one line
// each, with nothing a terminal would act on.
func TestModTextLoggedAsOneLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.log")
	r := &runner{log: openLog(path)}
	defer r.log.Close()
	note := func(method string, params any) {
		b, _ := json.Marshal(params)
		r.notified(&modproto.Message{Method: method, Params: b}, nil)
	}
	note(modproto.MethodLog, modproto.Log{Level: "info", Text: "ok\n2026-10-15T00:00:00.000Z broker: http.fetch GET https://x\x1b]52;c;eA==\x07\rgone"})
	note(modproto.MethodHandlerFailed, modproto.HandlerFailed{Where: "on\x1b[2J", Kind: "exception", Error: "bad\nline", Stack: "at a\nat b\x1b[1A"})
	note(modproto.MethodLog, modproto.Log{Level: "info", Text: strings.Repeat("é", maxText)})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d lines, want 5:\n%s", len(lines), b)
	}
	for _, want := range []string{
		`info: ok\n2026-10-15T00:00:00.000Z broker: http.fetch GET https://x\x1b]52;c;eA==\a\rgone`,
		`handler on\x1b[2J exception after 0 ms: bad\nline`,
		"  at a",
		`  at b\x1b[1A`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("log lacks %q:\n%s", want, b)
		}
	}
	for _, c := range string(b) {
		if c < ' ' && c != '\n' {
			t.Errorf("log has %q", c)
		}
	}
	if last := lines[4]; !strings.HasSuffix(last, "é…") {
		t.Errorf("long line cut mid-character: …%q", last[len(last)-8:])
	}
}
