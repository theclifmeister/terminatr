package peer

import (
	"errors"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home/hometest"
)

// TestPinTaken: a program's name is no alias (the programs design
// §3.4): --as refuses it, and a default alias steps past it.
func TestPinTaken(t *testing.T) {
	dir := hometest.PrivateDir(t)
	taken := func(n string) string {
		if n == "acme" {
			return "acme is a program"
		}
		return ""
	}
	_, err := PinTaken(dir, pub(newKey(t)), "wss://r", "studio", "acme", taken)
	var ne *NameError
	if !errors.As(err, &ne) || ne.Why != "acme is a program" {
		t.Fatalf("--as acme: %v", err)
	}
	p, err := PinTaken(dir, pub(newKey(t)), "wss://r", "acme.local", "", taken)
	if err != nil || p.Alias != "acme-2" {
		t.Fatalf("default alias %q: %v", p.Alias, err)
	}
}
