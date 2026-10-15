package client

import "testing"

func TestIsServerArgv(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want bool
	}{
		{[]string{"/opt/homebrew/bin/tm", "server", "run", "--detached"}, true},
		{[]string{"/Users/x/.terminatr/bin/tm-abc", "server", "run"}, true},
		{[]string{"tm", "server", "stop"}, false},
		{[]string{"server", "run"}, false}, // argv[0] is the program
		{[]string{"vim", "notes", "server", "run"}, false},
		{nil, false},
	} {
		if got := isServerArgv(c.argv); got != c.want {
			t.Errorf("%q: %v, want %v", c.argv, got, c.want)
		}
	}
}
