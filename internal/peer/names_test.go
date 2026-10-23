package peer

import (
	"errors"
	"path/filepath"
	"testing"
)

// A display name: set, renamed and cleared by either word; refused when
// another peer has it, or taken says so; Names maps both words to the
// alias and the alias to what shows.
func TestNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "peer")
	a, b := newKey(t), newKey(t)
	if err := SavePeers(dir, []Peer{{Alias: "del-02-0468-lt", Key: pub(a), Relay: "wss://r"}, {Alias: "mini", Key: pub(b), Relay: "wss://r"}}); err != nil {
		t.Fatal(err)
	}
	taken := func(n string) string {
		if n == "acme" {
			return "a project here is called that"
		}
		return ""
	}
	p, err := SetName(dir, "del-02-0468-lt", "work", taken)
	if err != nil || p.Name != "work" || p.Shown() != "work" || p.Both() != "work (del-02-0468-lt)" {
		t.Fatalf("set: %+v, %v", p, err)
	}
	n := LoadNames(dir)
	for in, want := range map[string]string{"work": "del-02-0468-lt", "del-02-0468-lt": "del-02-0468-lt", "mini": "mini", "nope": "nope"} {
		if got := n.Alias(in); got != want {
			t.Errorf("Alias(%s) = %s, want %s", in, got, want)
		}
	}
	if n.Key("work/gatehouse") != "del-02-0468-lt/gatehouse" || n.Key("gatehouse") != "gatehouse" || n.ShowKey("del-02-0468-lt/gatehouse") != "work/gatehouse" || n.Show("mini") != "mini" || n.Both("mini") != "mini" {
		t.Fatalf("Names: %+v", n)
	}

	// The name finds it too: rename by it.
	if p, err = SetName(dir, "work", "office", taken); err != nil || p.Alias != "del-02-0468-lt" || p.Name != "office" {
		t.Fatalf("rename: %+v, %v", p, err)
	}
	var ne *NameError
	for _, bad := range []string{"mini", "acme"} {
		if _, err := SetName(dir, "office", bad, taken); !errors.As(err, &ne) {
			t.Errorf("name %s: %v", bad, err)
		}
	}
	if _, err := SetName(dir, "mini", "office", taken); !errors.As(err, &ne) {
		t.Errorf("mini named office too: %v", err)
	}
	if _, err := SetName(dir, "office", "Bad Name", taken); err == nil || errors.As(err, &ne) {
		t.Errorf("bad characters: %v", err)
	}
	var unknown *UnknownPeerError
	if _, err := SetName(dir, "studio", "x", taken); !errors.As(err, &unknown) {
		t.Errorf("unknown peer: %v", err)
	}

	// Other changes take the name as well, and keep it.
	if ok, err := SetPanes(dir, "office", "view"); !ok || err != nil {
		t.Fatalf("panes by name: %v, %v", ok, err)
	}
	if ps, _ := LoadPeers(dir); ps[0].Panes != "view" || ps[0].Name != "office" {
		t.Fatalf("after panes: %+v", ps)
	}
	// Pairing again keeps it; a new pairing's alias steers clear of it.
	if p, err := Pin(dir, pub(a), "wss://r", "del-02-0468-lt", ""); err != nil || p.Name != "office" {
		t.Fatalf("pair again: %+v, %v", p, err)
	}
	if p, err := Pin(dir, pub(newKey(t)), "wss://r", "office.example", ""); err != nil || p.Alias != "office-2" {
		t.Fatalf("new pairing: %+v, %v", p, err)
	}
	if _, err := Pin(dir, pub(newKey(t)), "wss://r", "x", "office"); err == nil {
		t.Fatal("--as another peer's name")
	}

	// Cleared, by "" or by its alias: the alias shows again.
	if p, err = SetName(dir, "office", "", taken); err != nil || p.Name != "" || p.Shown() != "del-02-0468-lt" {
		t.Fatalf("clear: %+v, %v", p, err)
	}
	SetName(dir, "del-02-0468-lt", "work", taken)
	if p, err = SetName(dir, "work", "del-02-0468-lt", taken); err != nil || p.Name != "" {
		t.Fatalf("clear by alias: %+v, %v", p, err)
	}
	if ok, err := Unpin(dir, "mini"); !ok || err != nil {
		t.Fatal(ok, err)
	}

	// A file whose name collides doesn't load.
	if err := SavePeers(dir, []Peer{{Alias: "x", Name: "y", Key: pub(a), Relay: "wss://r"}, {Alias: "y", Key: pub(b), Relay: "wss://r"}}); err == nil {
		t.Fatal("saved a name that is another's alias")
	}
}
