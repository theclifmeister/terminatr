package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Peers checks the servers this one is paired with (docs/SPEC.md §16.8):
// nothing for a server never paired; else one line with each link's
// state, a warning when a link is refused, on an old protocol or can't
// reach its relay, a failure when the identity key or peers.json can't
// be read.
func Peers(d Deps, live Live) []Check {
	const g, name = "peers", "paired servers"
	dir := peer.DirIn(d.Paths.Home)
	id, err := peer.LoadIdentity(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []Check{{Group: g, Name: name, Status: Fail, Detail: "the identity key: " + err.Error() + "; the peers have it pinned (tm peer rotate makes a new one, then pair again)"}}
	}
	peers, err := peer.LoadPeers(dir)
	if err != nil {
		return []Check{{Group: g, Name: name, Status: Fail, Detail: err.Error()}}
	}
	if len(peers) == 0 {
		return []Check{{Group: g, Name: name, Status: OK, Detail: "server id " + id.ID.Short() + ", no peers"}}
	}
	if !live.Running {
		return []Check{{Group: g, Name: name, Status: OK,
			Detail: fmt.Sprintf("server id %s, %d paired; the server links them when it runs", id.ID.Short(), len(peers))}}
	}
	links := map[string]proto.PeerLink{}
	if live.Link != nil {
		for _, l := range live.Link.Peers {
			links[l.ID] = l
		}
	}
	st := OK
	var parts []string
	for _, p := range peers {
		l, ok := links[p.ID().String()]
		word := "no link"
		if ok {
			word = l.State
			if l.Reason != "" {
				word += " (" + l.Reason + ")"
			}
			switch l.Reason {
			case proto.LinkReasonRefused, proto.LinkReasonVersion, proto.LinkReasonRelay:
				st = Warn
				if l.Error != "" {
					word += ": " + l.Error
				}
			}
		}
		parts = append(parts, p.Alias+" "+word)
	}
	detail := "server id " + id.ID.Short() + ": " + strings.Join(parts, ", ")
	if st == Warn {
		detail += "; tm peer list, and docs/OPERATIONS.md, The servers' side"
	}
	return []Check{{Group: g, Name: name, Status: st, Detail: detail}}
}
