package ticker

import (
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// renderPeers renders the paired servers' stubs from the peer mirror
// when the vault is on and they changed since the last time
// (docs/SPEC.md §5.1, Vault).
func (t *Ticker) renderPeers(cfg *config.Config, list []project.Summary) {
	if t.o.Peers == nil || !vault.On() {
		return
	}
	var slugs []string
	for _, sum := range list {
		slugs = append(slugs, sum.Slug)
	}
	notes, keep := vault.PeerNotes(t.o.Peers(), vault.LinkedPeers(cfg, slugs))
	stamp := vault.PeersStamp(notes, keep)
	if stamp == t.peersStamp {
		return
	}
	if _, _, err := vault.RenderPeers(notes, keep); err != nil {
		t.o.Log.Printf("ticker: vault peers: %v", err)
		return // tried again next sweep
	}
	t.peersStamp = stamp
}
