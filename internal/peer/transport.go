package peer

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"

	"github.com/theclifmeister/terminatr/internal/relay"
)

// Transport carries a link's bytes to one peer. The link runs its own
// TLS with pinned keys inside, so a transport needn't be trusted: the
// relay is one; a direct or tunnelled address can be another.
type Transport interface {
	// Dial returns a stream to the peer, or fails. wait is called when
	// the transport is up but the peer isn't there yet; Dial goes on
	// waiting for it until ctx ends.
	Dial(ctx context.Context, wait func()) (net.Conn, error)
	// String names the transport for people: "relay relay.example.net".
	String() string
}

// relayTransport meets the peer in their link room on a relay
// (docs/SPEC.md §16.2).
type relayTransport struct {
	url  string
	host string
	d    relay.Dialer
}

// RelayTransport is the transport through the relay at rawURL for the
// link between id and peer. tc is the outer TLS to the relay: nil trusts
// the system's CAs.
func RelayTransport(rawURL string, id *Identity, peer ID, tc *tls.Config) Transport {
	host := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return &relayTransport{url: rawURL, host: host, d: relay.Dialer{
		Key:  id.Key,
		Kind: relay.KindLink,
		Room: relay.LinkRoom(id.ID, peer),
		Peer: peer,
		TLS:  tc,
	}}
}

func (t *relayTransport) Dial(ctx context.Context, wait func()) (net.Conn, error) {
	d := t.d
	d.OnWait = wait
	c, err := d.Dial(ctx, t.url)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (t *relayTransport) String() string { return "relay " + t.host }
