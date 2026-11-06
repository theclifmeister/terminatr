package ticker

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// sweepPeerFiles turns the files switch off for what no longer holds it: a
// project gone, archived or not shared, whichever way that came about, such
// as a hand-edited config. The commands that change those clear it themselves;
// this is what's left. A project that can't be read keeps it: its reads answer
// nothing meanwhile.
func (t *Ticker) sweepPeerFiles(cfg *config.Config, list []project.Summary) {
	dir, err := peer.Dir()
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, sum := range list {
		known[sum.Slug] = true
	}
	cleared, err := peer.ClearFiles(dir, func(slug string) bool {
		if !known[slug] {
			return true
		}
		s, err := cfg.Safety(slug)
		return err == nil && (!s.Share || s.Archived)
	})
	if err != nil {
		t.o.Log.Printf("ticker: files switch: %v", err)
		return
	}
	for _, x := range cleared {
		t.o.Log.Printf("ticker: files of %s off for %s: the project is gone, archived or not shared", x[1], x[0])
		if p, err := project.Open(x[1]); err == nil {
			p.Journal(caller.Caller{Kind: caller.Ticker}, "peer.files", x[0], "off: the project isn't shared")
		}
	}
}

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
