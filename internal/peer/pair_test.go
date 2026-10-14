package peer

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestCode(t *testing.T) {
	c, err := NewCode("wss://relay.example.net")
	if err != nil {
		t.Fatal(err)
	}
	s := c.String()
	if !regexp.MustCompile(`^relay\.example\.net/[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){3}$`).MatchString(s) {
		t.Fatalf("code %q", s)
	}
	back, err := ParseCode(s)
	if err != nil || back != c {
		t.Fatalf("ParseCode(%q) = %+v, %v", s, back, err)
	}
	// People retype codes: case, hyphens, spaces and look-alikes.
	var sec Secret
	for i := range sec {
		sec[i] = byte(i * 37)
	}
	want := Code{Relay: "wss://relay.example.net", Secret: sec}
	typed := strings.ToLower(strings.NewReplacer("0", "o", "1", "l", "-", " ").Replace(want.String()))
	if got, err := ParseCode("  " + typed + "\n"); err != nil || got != want {
		t.Fatalf("ParseCode(%q) = %+v, %v; want %+v", typed, got, err, want)
	}
	for in, relay := range map[string]string{
		"relay.example.net:8443/" + sec.String():     "wss://relay.example.net:8443",
		"wss://relay.example.net/v1/" + sec.String(): "wss://relay.example.net",
		"ws://127.0.0.1:8080/" + sec.String():        "ws://127.0.0.1:8080",
		"wss://h.example/tm/relay/" + sec.String():   "wss://h.example/tm/relay",
	} {
		got, err := ParseCode(in)
		if err != nil || got.Relay != relay || got.Secret != sec {
			t.Errorf("ParseCode(%q) = %+v, %v; want relay %s", in, got, err, relay)
		}
		if back, _ := ParseCode(got.String()); back != got {
			t.Errorf("%q: String %q doesn't read back", in, got.String())
		}
	}
	if s := (Code{Relay: "ws://127.0.0.1:8080", Secret: sec}).String(); !strings.HasPrefix(s, "ws://127.0.0.1:8080/") {
		t.Errorf("a ws relay's code %q", s)
	}
	for _, bad := range []string{"", "7Q4M-K2XD-9HVA-PT3C", "relay/7Q4M-K2XD-9HVA", "relay/7Q4M-K2XD-9HVA-PT3CX", "relay/7Q4M-K2XD-9HVA-PT3U", "https://r/7Q4M-K2XD-9HVA-PT3C"} {
		if _, err := ParseCode(bad); err == nil {
			t.Errorf("ParseCode(%q) took it", bad)
		}
	}
}

func TestCheckDigits(t *testing.T) {
	a, b := IDOf(pub(newKey(t))), IDOf(pub(newKey(t)))
	d := CheckDigits(a, b)
	if !regexp.MustCompile(`^\d{3} \d{3}$`).MatchString(d) || CheckDigits(a, b) != d {
		t.Fatalf("CheckDigits %q", d)
	}
	if CheckDigits(b, a) == d && CheckDigits(a, IDOf(pub(newKey(t)))) == d {
		t.Error("the digits don't depend on the keys")
	}
}

func TestAlias(t *testing.T) {
	for in, want := range map[string]string{
		"Mini.local": "mini", "studio": "studio", "Clif's MacBook Pro": "clif-s-macbook-pro", "": "peer", "...": "peer",
		strings.Repeat("x", 40): strings.Repeat("x", MaxAlias-3),
	} {
		if got := DefaultAlias(in); got != want || CheckAlias(got) != nil {
			t.Errorf("DefaultAlias(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "-x", "Mini", "a b", "a/b", strings.Repeat("x", MaxAlias+1)} {
		if CheckAlias(bad) == nil {
			t.Errorf("CheckAlias(%q) took it", bad)
		}
	}
}

func TestPin(t *testing.T) {
	s := newServer(t)
	a, b, c := pub(newKey(t)), pub(newKey(t)), pub(newKey(t))
	const r = "wss://relay.example.net"
	pin := func(k []byte, name, as string) Peer {
		t.Helper()
		p, err := Pin(s.dir, k, r, name, as)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	aliases := func() string {
		ps, err := LoadPeers(s.dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, p := range ps {
			out = append(out, p.Alias)
		}
		return strings.Join(out, " ")
	}
	if p := pin(a, "Mini.local", ""); p.Alias != "mini" {
		t.Fatalf("first: %s", p.Alias)
	}
	if p := pin(b, "mini", ""); p.Alias != "mini-2" {
		t.Fatalf("a second mini: %s", p.Alias)
	}
	// The same key again keeps its alias, whatever it calls itself now.
	if p := pin(a, "renamed", ""); p.Alias != "mini" || aliases() != "mini-2 mini" {
		t.Fatalf("again: %s, %s", p.Alias, aliases())
	}
	// --as replaces the peer called that: pairing again after it rotated.
	if p := pin(c, "whatever", "mini"); p.Alias != "mini" || aliases() != "mini-2 mini" {
		t.Fatalf("--as: %s, %s", p.Alias, aliases())
	}
	if ps, _ := LoadPeers(s.dir); !ps[1].Key.Equal(c) {
		t.Fatal("--as kept the old key")
	}
	if _, err := Pin(s.dir, a, r, "x", "Bad Alias"); err == nil {
		t.Fatal("pinned a bad alias")
	}
	if ok, err := Unpin(s.dir, "mini-2"); !ok || err != nil || aliases() != "mini" {
		t.Fatalf("Unpin: %v %v, %s", ok, err, aliases())
	}
	if ok, _ := Unpin(s.dir, "nope"); ok {
		t.Fatal("Unpin found nope")
	}
}

func TestRotateIdentity(t *testing.T) {
	s := newServer(t)
	id, err := RotateIdentity(s.dir)
	if err != nil || id.ID == s.id.ID {
		t.Fatalf("RotateIdentity: %v, same id %v", err, id.ID == s.id.ID)
	}
	if got, _ := LoadIdentity(s.dir); got.ID != id.ID {
		t.Fatal("the new key isn't the one on disk")
	}
}

// pairBoth runs a pairing between two servers through the relay at url,
// each side answering yes to its offer.
func pairBoth(t *testing.T, a, b *server, url string, o PairOptions, answerA, answerB bool) (offers [2]Offer, pa, pb *Paired, ea, eb error) {
	t.Helper()
	code, err := NewCode(url)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	waiting := make(chan struct{})
	done := make(chan struct{})
	oa := o
	oa.Name = "studio"
	oa.Waiting = func() { close(waiting) }
	oa.Confirm = func(of Offer) (bool, error) { offers[0] = of; return answerA, nil }
	go func() {
		defer close(done)
		pa, ea = Pair(ctx, a.id, code, oa)
	}()
	select {
	case <-waiting:
	case <-done:
		t.Fatalf("Pair: %v", ea)
	}
	ob := o
	ob.Name = "mini"
	ob.Confirm = func(of Offer) (bool, error) { offers[1] = of; return answerB, nil }
	back, _ := ParseCode(code.String())
	pb, eb = Join(ctx, b.id, back, ob)
	<-done
	return
}

// Two servers pair through a relay, pin each other and their link comes
// up.
func TestPairThroughRelay(t *testing.T) {
	_, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	o := PairOptions{Version: "0.27.0", RelayTLS: tc}
	offers, pa, pb, ea, eb := pairBoth(t, a, b, url, o, true, true)
	if ea != nil || eb != nil {
		t.Fatalf("pair: %v / %v", ea, eb)
	}
	if !pa.Key.Equal(pub(b.id.Key)) || !pb.Key.Equal(pub(a.id.Key)) || pa.Name != "mini" || pb.Name != "studio" {
		t.Fatalf("paired %+v / %+v", pa, pb)
	}
	if offers[0].Check != offers[1].Check || offers[0].Check != CheckDigits(a.id.ID, b.id.ID) {
		t.Fatalf("checks %q and %q", offers[0].Check, offers[1].Check)
	}
	if offers[0].ID != b.id.ID || offers[1].ID != a.id.ID || offers[0].Version != "0.27.0" {
		t.Fatalf("offers %+v", offers)
	}
	if _, err := Pin(a.dir, pa.Key, url, pa.Name, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Pin(b.dir, pb.Key, url, pb.Name, ""); err != nil {
		t.Fatal(err)
	}
	ma, mb := a.start(t, "0.27.0", tc), b.start(t, "0.27.0", tc)
	waitLink(t, ma, "mini", "up", isUp)
	waitLink(t, mb, "studio", "up", isUp)
}

func TestPairDeclined(t *testing.T) {
	_, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	_, _, _, ea, eb := pairBoth(t, a, b, url, PairOptions{RelayTLS: tc}, true, false)
	if !errors.Is(ea, ErrPeerNo) || !errors.Is(eb, ErrDeclined) {
		t.Fatalf("got %v / %v", ea, eb)
	}
}

func TestJoinNobody(t *testing.T) {
	_, _, url, tc := startRelay(t)
	code, _ := NewCode(url)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Join(ctx, newServer(t).id, code, PairOptions{RelayTLS: tc, Confirm: func(Offer) (bool, error) { t.Fatal("asked"); return false, nil }})
	if !errors.Is(err, ErrNoPairer) {
		t.Fatalf("got %v", err)
	}
}

// Someone in the middle without the secret fails the proof: here the
// two sides simply hold different secrets over a pipe.
func TestPairWrongSecret(t *testing.T) {
	a, b := newServer(t), newServer(t)
	ca, cb := tcpPipe(t)
	var t1, t2 Secret
	t2[0] = 1
	never := PairOptions{Confirm: func(Offer) (bool, error) { t.Error("asked"); return false, nil }}
	errs := make(chan error, 1)
	go func() {
		_, err := runPair(ca, a.id, t1, rolePair, never)
		errs <- err
	}()
	_, eb := runPair(cb, b.id, t2, roleJoin, never)
	ea := <-errs
	if !errors.Is(ea, ErrBadCode) && !errors.Is(eb, ErrBadCode) {
		t.Fatalf("got %v / %v", ea, eb)
	}
}

// A server can't pair with itself.
func TestPairOwnKey(t *testing.T) {
	a := newServer(t)
	ca, cb := tcpPipe(t)
	var sec Secret
	never := PairOptions{Confirm: func(Offer) (bool, error) { t.Error("asked"); return false, nil }}
	errs := make(chan error, 1)
	go func() {
		_, err := runPair(ca, a.id, sec, rolePair, never)
		errs <- err
	}()
	_, eb := runPair(cb, a.id, sec, roleJoin, never)
	if ea := <-errs; !errors.Is(ea, ErrOwnKey) || !errors.Is(eb, ErrOwnKey) {
		t.Fatalf("got %v / %v", ea, eb)
	}
}

// tcpPipe is two ends of a loopback TCP connection: buffered, as the
// relay is, where net.Pipe would block both sides' first writes.
func tcpPipe(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	acc := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		acc <- c
	}()
	a, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b := <-acc
	if b == nil {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}
