package ticker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestRenderPeers (T724): a sweep writes a paired server's stubs from
// the mirror once the vault is on, and again only when they change.
func TestRenderPeers(t *testing.T) {
	r := newRig(t)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	peers := []proto.PeerMirror{{Alias: "mini", State: proto.LinkUp, Listed: true,
		Projects: []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "acme-api"}}}}}
	r.tk.o.Peers = func() []proto.PeerMirror { return peers }
	d, _ := home.Dir()
	stub := filepath.Join(d, "projects", "_peers", "mini", "acme-api.md")
	r.sweep(time.Second)
	if _, err := os.Stat(stub); err == nil {
		t.Fatal("a stub before the vault is on")
	}
	os.WriteFile(filepath.Join(d, "state", "vault"), nil, 0o600)
	r.sweep(time.Second)
	if _, err := os.Stat(stub); err != nil {
		t.Fatalf("no stub: %v", err)
	}
	peers[0].Projects = nil
	r.sweep(time.Second)
	if _, err := os.Stat(stub); err == nil {
		t.Fatal("the stub of a project no longer shared stayed")
	}
}
