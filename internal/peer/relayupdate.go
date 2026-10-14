package peer

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/theclifmeister/terminatr/internal/relay"
)

// RequestRelayUpdate asks the relay of the paired server alias to update
// itself to the latest official release (docs/SPEC.md §16.2), with this
// server's identity key: the relay takes it while the link to alias is
// live through it, at most once an hour. tc is the outer TLS to the
// relay: nil trusts the system's CAs.
func RequestRelayUpdate(ctx context.Context, dir, alias string, tc *tls.Config) (Peer, relay.UpdateResult, error) {
	id, err := LoadIdentity(dir)
	if err != nil {
		return Peer{}, relay.UpdateResult{}, err
	}
	peers, err := LoadPeers(dir)
	if err != nil {
		return Peer{}, relay.UpdateResult{}, err
	}
	for _, p := range peers {
		if p.Alias != alias {
			continue
		}
		d := relay.Dialer{Key: id.Key, Room: relay.LinkRoom(id.ID, p.ID()), Peer: p.ID(), TLS: tc}
		res, err := d.RequestUpdate(ctx, p.Relay)
		return p, res, err
	}
	return Peer{}, relay.UpdateResult{}, &UnknownPeerError{alias}
}

// UnknownPeerError means no paired server has the alias.
type UnknownPeerError struct{ Alias string }

func (e *UnknownPeerError) Error() string {
	return fmt.Sprintf("no paired server %q (tm peer list)", e.Alias)
}

// RelayUpdateWords says what came of an update request, for people.
func RelayUpdateWords(host string, r relay.UpdateResult) (string, bool) {
	switch r.Result {
	case relay.UpdateQueued:
		return fmt.Sprintf("the relay %s updates to the latest release if it is newer; its links drop for a few seconds while it restarts", host), true
	case relay.UpdateOff:
		return fmt.Sprintf("the relay %s takes no update requests: it was installed before them; run sudo tm relay install again there (or sudo tm relay update)", host), false
	case relay.UpdateNotLinked:
		return fmt.Sprintf("the relay %s takes update requests only from a server whose link through it is up; this one's isn't", host), false
	case relay.UpdateTooSoon:
		return fmt.Sprintf("the relay %s took an update request less than an hour ago; it takes another after %s", host, r.Next.Local().Format("15:04")), false
	}
	return fmt.Sprintf("the relay %s couldn't write the update request; its log says why (journalctl -u tm-relay)", host), false
}
