package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

func TestLinkWordsTextIsPlain(t *testing.T) {
	out := linkWords(proto.PeerLink{State: proto.LinkUp, Version: "v1\x1b]52;c;AAAA\x07", Since: time.Now()}, time.Now())
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Fatalf("%q", out)
	}
	out = linkWords(proto.PeerLink{State: proto.LinkOffline, Error: "e\x1b]0;x\x07\x1b[31m", Since: time.Now()}, time.Now())
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Fatalf("%q", out)
	}
}
