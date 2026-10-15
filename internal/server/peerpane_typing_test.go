package server

import "testing"

// TestTyping: a paired server's console sends focus and mouse reports
// with no one typing; only keys, pastes and mouse presses are input.
func TestTyping(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"a", true},
		{"\r", true},
		{"\x1b[A", true},
		{"\x1b[200~hi\x1b[201~", true},
		{"\x1b[I", false},
		{"\x1b[O", false},
		{"\x1b[I\x1b[O", false},
		{"\x1b[<0;10;5M", true},
		{"\x1b[<0;10;5m", false},
		{"\x1b[<35;10;5M", false},
		{"\x1b[<64;10;5M", false},
		{"\x1b[M #!", true},
		{"\x1b[M#!!", false},
		{"\x1b[I\x1b[<0;1;1M", true},
		{"\x1b[Ix", true},
	} {
		if got := typing([]byte(c.in)); got != c.want {
			t.Errorf("typing(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
