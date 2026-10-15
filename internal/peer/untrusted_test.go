package peer

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

func TestHelloTextIsCleaned(t *testing.T) {
	h := Hello{LinkProtocol: 1, Version: "v0.28.0\x1b]52;c;AAAA\x07\x1b[2J", Caps: []string{"pane", "x\x1b[31m", strings.Repeat("a", 99), "msg"}}.clean()
	if strings.ContainsAny(h.Version, "\x1b\x07") {
		t.Fatalf("version %q", h.Version)
	}
	if len(h.Caps) != 2 || h.Caps[0] != "pane" || h.Caps[1] != "msg" {
		t.Fatalf("caps %q", h.Caps)
	}
	many := make([]string, 100)
	for i := range many {
		many[i] = "cap"
	}
	if got := (Hello{Caps: many}).clean().Caps; len(got) != maxCaps {
		t.Fatalf("%d caps", len(got))
	}
}

func TestLinkErrorIsCleaned(t *testing.T) {
	var logged []string
	l := &link{logf: func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }}
	l.set(proto.LinkOffline, proto.LinkReasonRelay, errors.New("closed: \x1b]0;owned\x07\x1b[31mfake (4400)"), time.Time{})
	if e := l.status().Error; strings.ContainsAny(e, "\x1b\x07") || e == "" {
		t.Fatalf("error %q", e)
	}
	if len(logged) == 0 || strings.ContainsAny(strings.Join(logged, ""), "\x1b\x07") {
		t.Fatalf("log %q", logged)
	}
}
