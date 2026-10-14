package caller

import "testing"

func TestNarrower(t *testing.T) {
	human := Caller{Kind: Human}
	coord := Caller{Kind: Coordinator, Project: "p"}
	thread := Caller{Kind: Thread, Project: "p", Thread: "t-0001"}
	cases := []struct{ env, srv, want Caller }{
		{human, human, human},
		{human, coord, coord},   // variables unset: the server still knows
		{coord, human, coord},   // left its session's tree: the env still says
		{coord, thread, thread}, // the narrower wins
		{thread, coord, thread},
		{Caller{Kind: Thread, Thread: "t-9"}, thread, thread}, // a tie: the server's ids
	}
	for _, c := range cases {
		if got := Narrower(c.env, c.srv); got != c.want {
			t.Errorf("Narrower(%v, %v) = %v, want %v", c.env, c.srv, got, c.want)
		}
	}
}

func TestMod(t *testing.T) {
	m := ForMod("auto-labeller")
	if !m.IsAgent() {
		t.Error("a mod passes human-only checks")
	}
	if got := m.String(); got != "mod:auto-labeller" {
		t.Errorf("String() = %q", got)
	}
	human := Caller{Kind: Human}
	if got := Narrower(m, human); got != m {
		t.Errorf("Narrower(mod, human) = %v", got)
	}
	if got := Narrower(m, Caller{Kind: Thread, Thread: "t-1"}); got != m {
		t.Errorf("a thread is narrower than a mod: %v", got)
	}
}

func TestPeer(t *testing.T) {
	p := ForPeer("mini")
	if !p.IsAgent() {
		t.Error("a paired server passes human-only checks")
	}
	if got := p.String(); got != "peer:mini" {
		t.Errorf("String() = %q", got)
	}
	for _, k := range []Kind{Human, Coordinator, Thread, Mod} {
		if got := Narrower(p, Caller{Kind: k}); got != p {
			t.Errorf("%s is narrower than a paired server: %v", k, got)
		}
	}
	// No environment names a paired server.
	env := map[string]string{EnvSession: "s-1", EnvRole: "peer"}
	if got := FromLookup(func(k string) string { return env[k] }); got.Kind == Peer {
		t.Errorf("the environment made a peer: %v", got)
	}
}
