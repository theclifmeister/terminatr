package device

// Pairing a device (docs/SPEC.md §16.15): the server waits in a pair
// room on its relay and shows a QR code, an Invite; the phone scans it,
// joins the room, takes only the key the invite names and proves it
// knows the secret, as two servers do (peer.Pair). The camera is the
// side channel, so nobody compares digits; the user confirms the
// device's name on the server before anything is pinned.

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/theclifmeister/terminatr/internal/peer"
)

// InviteScheme and InviteHost start an invite's URL: a phone's camera
// opens terminatr://pair?… in the app.
const (
	InviteScheme = "terminatr"
	InviteHost   = "pair"
	// InviteVersion is the invite's format, its v.
	InviteVersion = "1"
)

// Invite is what a device's QR code carries: the relay to meet on (with
// its pin when it has one), the one-time secret and this server's id.
type Invite struct {
	Code   peer.Code
	Server peer.ID
}

// NewInvite is a fresh invite through relayURL (peer.RelayURL's form)
// for the server id.
func NewInvite(relayURL string, id peer.ID) (Invite, error) {
	c, err := peer.NewCode(relayURL)
	return Invite{Code: c, Server: id}, err
}

// String is the invite as its QR code holds it:
// terminatr://pair?v=1&relay=<URL>&secret=<16 digits>&server=<64 hex>.
func (in Invite) String() string {
	q := url.Values{}
	q.Set("v", InviteVersion)
	q.Set("relay", in.Code.Relay)
	q.Set("secret", in.Code.Secret.String())
	q.Set("server", in.Server.String())
	// url.Values sorts its keys; v goes first so a reader sees it first
	return InviteScheme + "://" + InviteHost + "?v=" + InviteVersion + "&" + strings.TrimPrefix(q.Encode(), "v="+InviteVersion+"&")
}

// ParseInvite reads an invite back.
func ParseInvite(s string) (Invite, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != InviteScheme || u.Host != InviteHost {
		return Invite{}, errors.New("not a terminatr pairing code (terminatr://pair?…)")
	}
	q := u.Query()
	if q.Get("v") != InviteVersion {
		return Invite{}, fmt.Errorf("pairing code version %q: this tm reads %s", q.Get("v"), InviteVersion)
	}
	r, err := peer.RelayURL(q.Get("relay"))
	if err != nil {
		return Invite{}, err
	}
	sec, ok := peer.ParseSecret(q.Get("secret"))
	if !ok {
		return Invite{}, errors.New("the pairing code's secret isn't 16 digits and letters")
	}
	var id peer.ID
	b, err := hex.DecodeString(q.Get("server"))
	if err != nil || len(b) != len(id) {
		return Invite{}, errors.New("the pairing code's server isn't 64 hex digits")
	}
	copy(id[:], b)
	return Invite{Code: peer.Code{Relay: r, Secret: sec}, Server: id}, nil
}

// Offer is what the user confirms on the server: the device's name, as
// it gave it, its id, its app's version and its write key (nil when it
// sent none, or one that isn't a P-256 point: it can only view).
type Offer struct {
	Name     string
	Version  string
	ID       peer.ID
	WriteKey []byte
}

// PairOptions configure the server's side of a device pairing.
type PairOptions struct {
	// Name and Version are this server's, sent to the device.
	Name, Version string
	// RelayTLS is the outer TLS to the relay: nil trusts the system's CAs.
	RelayTLS *tls.Config
	// Confirm shows the offer and returns the user's answer.
	Confirm func(Offer) (bool, error)
	// Waiting is called once the server waits in the room: from then on
	// the code works, so that is when to show it.
	Waiting func()
}

// Pair waits in the pair room of in for a device to join, then runs the
// pairing: the device the user said yes to. ctx bounds the wait.
func Pair(ctx context.Context, id *peer.Identity, in Invite, o PairOptions) (*peer.Paired, error) {
	if in.Server != id.ID {
		return nil, errors.New("the invite names another server's key")
	}
	p, err := peer.Pair(ctx, id, in.Code, peer.PairOptions{
		Name: o.Name, Version: o.Version, RelayTLS: o.RelayTLS, Waiting: o.Waiting, Purpose: peer.PurposeDevice,
		Confirm: func(po peer.Offer) (bool, error) {
			return o.Confirm(Offer{Name: CleanName(po.Name), Version: po.Version, ID: po.ID, WriteKey: writeKeyOf(po.WriteKey)})
		},
	})
	if err != nil {
		return nil, err
	}
	p.WriteKey = writeKeyOf(p.WriteKey)
	return p, nil
}

// writeKeyOf is b when it is a write key, else nil.
func writeKeyOf(b []byte) []byte {
	if CheckWriteKey(b) != nil {
		return nil
	}
	return b
}

// Join is the device's side, as the phone runs it: it joins the pairing
// in names with the device's key, takes only the server key in names,
// and gives its name and write key (nil for none). It returns the
// server, once the user said yes there.
func Join(ctx context.Context, key *peer.Identity, in Invite, name, version string, writeKey []byte, relayTLS *tls.Config) (*peer.Paired, error) {
	return peer.Join(ctx, key, in.Code, peer.PairOptions{
		Name: name, Version: version, RelayTLS: relayTLS, Purpose: peer.PurposeDevice, Expect: &in.Server, WriteKey: writeKey,
		Confirm: func(peer.Offer) (bool, error) { return true, nil },
	})
}
