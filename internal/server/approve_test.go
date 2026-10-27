package server

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestScopeAllowed: once is anyone's to pick, the session only the
// user's in the Mac app, once confirmed, and beyond it nobody's.
func TestScopeAllowed(t *testing.T) {
	for _, c := range []struct {
		scope agent.Scope
		p     proto.SessionApproveParams
		app   bool
		want  string
	}{
		{agent.ScopeOnce, proto.SessionApproveParams{}, false, ""},
		{agent.ScopeOnce, proto.SessionApproveParams{}, true, ""},
		{agent.ScopeSession, proto.SessionApproveParams{Session: true}, false, proto.ErrAlwaysAllow},
		{agent.ScopeSession, proto.SessionApproveParams{}, true, proto.ErrSessionUnconfirmed},
		{agent.ScopeSession, proto.SessionApproveParams{Check: true}, true, ""},
		{agent.ScopeSession, proto.SessionApproveParams{Session: true}, true, ""},
		{agent.ScopePersistent, proto.SessionApproveParams{Session: true}, true, proto.ErrAlwaysAllow},
		{agent.ScopePersistent, proto.SessionApproveParams{Check: true}, true, proto.ErrAlwaysAllow},
	} {
		got := ""
		if perr := scopeAllowed(c.scope, 2, "Yes, and …", c.p, c.app); perr != nil {
			got = perr.Code
		}
		if got != c.want {
			t.Errorf("%s %+v app=%v: %q, want %q", c.scope, c.p, c.app, got, c.want)
		}
	}
}
