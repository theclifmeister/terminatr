package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// phoneMasks hide what differs between runs on a phone's pairing
// screens: the QR code (its rows, with the blanks around them), the
// invite as text, the time left, and the host's name; pairMasks the
// relay's port and the short ids.
var phoneMasks = []Mask{
	{Name: "qr", Re: regexp.MustCompile(` *[█▀▄][█▀▄ ]*`)},
	{Name: "invite", Re: regexp.MustCompile(`[0-9A-Za-z%&=:/?._-]{30,} *`), Repl: "<invite> "},
	{Name: "left", Re: regexp.MustCompile(`\b\d+:\d\d\b`)},
	{Name: "server", Re: regexp.MustCompile(`This server: \S+, id \S+ *`), Repl: "This server: <host>, id <id> "},
}

// inviteRe finds the invite under "Or paste this code in the app:",
// wrapped over the popup's lines.
var inviteRe = regexp.MustCompile(`(?s)Or paste this code in the app:(.*?)(?:\n *│ *│|\n *╰)`)

// screenInvite is the invite the pairing popup shows as text: its lines
// joined, without the popup's borders.
func screenInvite(t *testing.T, screen string) device.Invite {
	t.Helper()
	m := inviteRe.FindStringSubmatch(screen)
	if m == nil {
		t.Fatalf("no invite on screen:\n%s", screen)
	}
	var b strings.Builder
	for _, l := range strings.Split(m[1], "\n") {
		b.WriteString(strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "│")))
	}
	in, err := device.ParseInvite(b.String())
	if err != nil {
		t.Fatalf("invite %q: %v", b.String(), err)
	}
	return in
}

// TestPhonePairTUI (T828): the Network popup pairs a phone as tm device
// pair does. With no relay known, D asks for one; the popup shows the
// QR code and the invite as text, which the test's phone joins with;
// the popup asks, E names it, Y adds it and the popup closes with the
// outcome, the phone listed. Its link comes up, then drops: last seen.
// V sets its level (tm device list says it), E renames it, X removes it
// after asking.
func TestPhonePairTUI(t *testing.T) {
	env := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	relayURL := "ws" + strings.TrimPrefix(hs.URL, "http")
	env.FakeClaude()

	w := env.Window(120, 50)
	w.WaitFor("+ new project", wait)
	w.Type("N")
	w.WaitFor("No phone yet. D pairs one", wait)
	w.Type("D")
	w.WaitFor("The phone and this server meet on a relay", wait)
	w.Keys(relayURL, keyEnter)
	screen := w.WaitFor("waiting for the phone", wait)
	if !strings.Contains(screen, "▀") || !strings.Contains(screen, "▄") {
		t.Fatalf("no QR code:\n%s", screen)
	}
	masks := slices.Concat(phoneMasks, popupMasks, pairMasks, networkMasks)
	w.Golden("phone-pair-code.txt", masks...)
	in := screenInvite(t, screen)

	// The phone joins with the invite.
	_, phoneKey, _ := ed25519.GenerateKey(nil)
	wk, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wkb, _ := wk.PublicKey.Bytes()
	joined := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := device.Join(ctx, peer.IdentityOf(phoneKey), in, "Test iPhone", "1.0", wkb, nil)
		joined <- err
	}()
	w.WaitFor("A phone joined", wait)
	w.Golden("phone-pair-offer.txt", masks...)
	w.Type("E")
	w.WaitFor("Add it as", wait)
	w.Keys("\x15", "Work phone", keyEnter)
	w.WaitFor(`Add it as "Work phone", a view-only device?`, wait)
	w.Type("Y")
	w.WaitFor("paired Work phone (device id", wait)
	if err := <-joined; err != nil {
		t.Fatalf("the phone's join: %v", err)
	}
	if out := env.MustCLI("device", "list"); !strings.Contains(out, "Work phone") || !strings.Contains(out, " view ") {
		t.Fatalf("device list:\n%s", out)
	}

	// Its link comes up, then drops.
	srv, err := peer.LoadIdentity(peer.DirIn(env.Home))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := device.Dial(ctx, peer.IdentityOf(phoneKey), srv.Key.Public().(ed25519.PublicKey), in.Code.Relay, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.WaitFor("Work phone · connected", wait)
	w.Golden("phone-list.txt", masks...)
	conn.Close()
	w.WaitFor("Work phone · last seen", wait)

	// V: answer; tm device list says it.
	w.Type("V")
	w.WaitFor("Level for Work phone", wait)
	w.Golden("phone-level.txt", masks...)
	w.Keys(keyDown, keyEnter)
	w.WaitFor("Work phone is answer now", wait)
	if out := env.MustCLI("device", "list"); !strings.Contains(out, " answer ") {
		t.Fatalf("device list after V:\n%s", out)
	}

	// E renames it; X removes it after asking.
	w.Type("E")
	w.WaitFor("Name for Work phone", wait)
	w.Keys("\x15", "Old phone", keyEnter)
	w.WaitFor("Work phone is called Old phone now", wait)
	w.Type("X")
	w.WaitFor("Remove Old phone?", wait)
	w.Type("Y")
	w.WaitFor("removed Old phone", wait)
	w.WaitFor("No phone yet", wait)
	if out := env.MustCLI("device", "list"); !strings.Contains(out, "no devices") {
		t.Fatalf("device list after X:\n%s", out)
	}
}
