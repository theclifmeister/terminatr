package e2e

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// pairMasks hide what differs between runs on a pairing's screens: the
// relay's port, the code, the six digits, server ids, the machine's
// name, tm's version and the time the code has left, with the padding
// after those whose width varies.
var pairMasks = []Mask{
	{Name: "code", Re: regexp.MustCompile(`[0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4} *`), Repl: "<code> "},
	{Name: "port", Re: regexp.MustCompile(`127\.0\.0\.1:\d+`)},
	{Name: "check", Re: regexp.MustCompile(`\d{3} \d{3}`)},
	{Name: "id", Re: regexp.MustCompile(`\b[0-9A-F]{4}-[0-9A-F]{4}\b`)},
	{Name: "host", Re: regexp.MustCompile(`The other server: \S+ *`), Repl: "The other server: <host> "},
	{Name: "left", Re: regexp.MustCompile(`\d+:\d\d left\): *`), Repl: "<left>): "},
	{Name: "version", Re: regexp.MustCompile(` · tm \S+ *`), Repl: " · tm <version> "},
}

var (
	pairCodeRe  = regexp.MustCompile(`ws://127\.0\.0\.1:\d+/[0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4}`)
	pairCheckRe = regexp.MustCompile(`Check: (\d{3} \d{3})`)
	pairedRe    = regexp.MustCompile(`paired with (\S+); its link comes up`)
)

// TestSmokePeerPairSettings: studio pairs from the settings' Peers tab
// (n, the relay, the code shown to pass on) while mini joins with tm
// peer join; both see the same six digits, y pins each, and the tab
// lists mini, linked. Then the other way: mini runs tm peer pair and studio
// joins its code with j.
func TestSmokePeerPairSettings(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	relayURL := "ws" + strings.TrimPrefix(hs.URL, "http")

	mini.MustCLI("server", "start")
	studio.FakeClaude() // no "no agent installed" alarm over the header

	w := studio.Window(110, 30)
	w.WaitFor("n new project", wait)
	w.Keys(",", "5")
	w.WaitFor("Press n to pair", wait)
	w.Golden("peers-empty.txt", popupMasks...)

	w.Type("n")
	w.WaitFor("The relay both servers dial", wait)
	w.Keys(relayURL, keyEnter)
	screen := w.WaitFor("Waiting for the other server", wait)
	code := pairCodeRe.FindString(screen)
	if code == "" {
		t.Fatalf("no code on screen:\n%s", screen)
	}
	w.Golden("peers-pair-code.txt", append(append([]Mask{}, popupMasks...), pairMasks...)...)

	join := startPairing(t, mini, "join", code, "--as", "studio")
	checkB := join.waitLine(`^Check: (\d{3} \d{3})$`)[1]
	screen = w.WaitFor("Pair only if the other side shows", wait)
	if m := pairCheckRe.FindStringSubmatch(screen); m == nil || m[1] != checkB {
		t.Fatalf("studio's check isn't mini's %s:\n%s", checkB, screen)
	}
	w.Golden("peers-pair-check.txt", append(append([]Mask{}, popupMasks...), pairMasks...)...)
	join.waitLine(`Do they match\? \[y/N\] $`)
	join.answer("y")
	w.Type("y")
	// The popup takes no alias: mini is called after its machine.
	screen = w.WaitFor("; its link comes up in a moment", wait)
	alias := pairedRe.FindStringSubmatch(screen)[1]
	if c, out := join.wait(); c != 0 || !strings.Contains(out, "Paired with studio") {
		t.Fatalf("join: exit %d\n%s", c, out)
	}
	waitPeerState(t, studio, alias, proto.LinkUp, "")
	w.WaitFor(" linked · ", wait)

	// The other way: mini pairs, studio joins with j.
	pair := startPairing(t, mini, "pair", "--relay", relayURL, "--as", "studio")
	code = pair.waitLine(`^On the other server: tm peer join (\S+)$`)[1]
	w.Type("j")
	w.WaitFor("The pairing code the other server shows", wait)
	w.Keys(code, keyEnter)
	checkA := pair.waitLine(`^Check: (\d{3} \d{3})$`)[1]
	screen = w.WaitFor("Pair only if the other side shows", wait)
	if m := pairCheckRe.FindStringSubmatch(screen); m == nil || m[1] != checkA {
		t.Fatalf("studio's check isn't mini's %s:\n%s", checkA, screen)
	}
	pair.waitLine(`Do they match\? \[y/N\] $`)
	pair.answer("y")
	w.Type("y")
	w.WaitFor("paired with "+alias+";", wait)
	if c, out := pair.wait(); c != 0 || !strings.Contains(out, "Paired with studio") {
		t.Fatalf("pair: exit %d\n%s", c, out)
	}
	// Pairing again kept the one entry and its alias.
	if out := studio.MustCLI("peer", "list"); strings.Count(out, alias) != 1 {
		t.Fatalf("peer list:\n%s", out)
	}
}
