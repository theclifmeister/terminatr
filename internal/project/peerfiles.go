package project

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/peer"
)

// The files switch's changes in the project's journal (docs/SPEC.md §16.18):
// the switch, not each read. It lives with the paired servers (peer.Peer),
// and goes when the project stops being what the user agreed to show: when
// sharing goes off, and when the project is archived, deleted or renamed.

// SetPeerFiles turns the files switch for the paired server alias on or off
// for this project and journals it (peer.files, on or off); false when there
// is no such server.
func (p *Project) SetPeerFiles(c caller.Caller, dir, alias string, on bool) (bool, error) {
	found, err := peer.SetFiles(dir, alias, p.Slug, on)
	if err != nil || !found {
		return found, err
	}
	return true, p.Journal(c, "peer.files", alias, map[bool]string{true: "on", false: "off"}[on])
}

// ForgetPeerFiles turns slug's files switch off for every paired server and
// journals each in p's journal, with why. Sharing again doesn't bring it back.
func ForgetPeerFiles(c caller.Caller, p *Project, slug, why string) error {
	dir, err := peer.Dir()
	if err != nil {
		return err
	}
	cleared, err := peer.ClearFiles(dir, func(s string) bool { return s == slug })
	if err != nil {
		return err
	}
	for _, x := range cleared {
		if jerr := p.Journal(c, "peer.files", x[0], "off: "+why); jerr != nil {
			err = jerr
		}
	}
	return err
}
