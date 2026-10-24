package tui

import (
	"fmt"
	"github.com/theclifmeister/terminatr/internal/textsafe"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/update"
)

// The Network popup's relay version and its U (docs/SPEC.md §16.2): each
// link says which tm its relay runs, "update available" when the
// latest release this server knows of (its own update check) is newer,
// and U asks the selected server's relay to update itself, as tm peer
// relay-update does.

// relayBehind says whether a relay running tm v is older than the latest
// release the server knows of: a newer one its update check found, else
// its own (a release isn't older than the latest as last checked).
func relayBehind(si ServerInfo, v string) (latest string, behind bool) {
	latest = si.Latest
	if latest == "" {
		latest = si.Version
	}
	newer, ok := update.Newer(latest, v)
	return latest, ok && newer
}

// relayUpdate asks, then has the selected server's relay update itself.
func (nv *networkView) relayUpdate(m *dash, e PeerEntry) {
	host := relayWords(e.Relay)
	q := fmt.Sprintf("Ask the relay %s to update itself to the latest official release? It restarts only for a newer one, and every link through it drops for a few seconds; it takes one request an hour.", host)
	if e.Link != nil && e.Link.RelayVersion != "" {
		if latest, behind := relayBehind(m.data.Server, e.Link.RelayVersion); behind {
			q = fmt.Sprintf("The relay %s runs tm %s; %s is out. ", host, textsafe.Line(e.Link.RelayVersion, 40), latest) + q
		}
	}
	m.confirmNo("Update the relay", q, "relay "+host+" left as it is", func() tea.Cmd {
		src, alias := m.src, e.Alias
		return m.act(func() actionMsg {
			msg, err := src.RelayUpdate(alias)
			return actionMsg{err: err, msg: msg, then: nv.reload}
		})
	})
}
