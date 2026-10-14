package tasks

import (
	"regexp"
	"strconv"
	"testing"
)

func TestValidRef(t *testing.T) {
	for s, want := range map[string]bool{
		"T1": true, "T12": true, "T999999999": true,
		"T0": false, "T012": false, "t12": false, "12": false, "T": false,
		"T1000000000": false, " T12": false, "T12 ": false, "T1a": false, "T-1": false,
	} {
		if ValidRef(s) != want {
			t.Errorf("ValidRef(%q) = %v", s, !want)
		}
		// Every valid ref parses to its id.
		if n, ok := ParseRef(s); want && (!ok || "T"+strconv.Itoa(n) != s) {
			t.Errorf("ParseRef(%q) = %d, %v", s, n, ok)
		}
	}
}

func TestValidThreadID(t *testing.T) {
	for s, want := range map[string]bool{
		"t-0001": true, "t-12345": true,
		"t-001": false, "T-0001": false, "t0001": false, "t-00a1": false, "t-0001/x": false, "": false,
	} {
		if ValidThreadID(s) != want {
			t.Errorf("ValidThreadID(%q) = %v", s, !want)
		}
	}
}

// The patterns compose: a label finds the ref and id inside a line.
func TestPatternsCompose(t *testing.T) {
	re := regexp.MustCompile(`^(` + RefPattern + `) .* \((` + ThreadIDPattern + `)\)$`)
	if m := re.FindStringSubmatch("T12 Title (t-0003)"); m == nil || m[1] != "T12" || m[2] != "t-0003" {
		t.Fatalf("got %q", m)
	}
}
