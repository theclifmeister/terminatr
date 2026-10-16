// Package agenttest is what the agent adapters' tests share.
package agenttest

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// Load is the embedded agent called name, with its manifest.
func Load(t *testing.T, name string) agent.Agent {
	t.Helper()
	r, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := r.Get(name)
	if !ok {
		t.Fatalf("no %s agent", name)
	}
	if agent.ManifestOf(a) == nil {
		t.Fatalf("ManifestOf(%s) is nil", name)
	}
	return a
}
