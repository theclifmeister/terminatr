package tui

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/version"
)

// ServerSource's PeerSource: the paired servers.

// PeerList is the paired servers, for the Network popup (network.go):
// this server's id ("" before it has an identity key)
// and each paired server with its link as the server has it.
type PeerList struct {
	ID, Short string
	Peers     []PeerEntry
	// Running: a server answered; LinkErr is why it can't read the
	// identity key or the peers.
	Running bool
	LinkErr string
}

// PeerEntry is one paired server: the alias the user knows it by, the
// display name given it here ("" for none), its server id and relay,
// and its link (nil while there is none).
type PeerEntry struct {
	Alias, Name, ID, Short, Relay string
	Link                          *proto.PeerLink
	// Panes is this server's pane switch for it (proto.PanesOff …).
	Panes string
}

func (s *ServerSource) Peers() (PeerList, error) {
	var l PeerList
	if s.Caller.IsAgent() {
		return l, errHumanOnly
	}
	dir := peer.DirIn(s.Paths.Home)
	id, err := peer.LoadIdentity(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return l, err
	}
	if id != nil {
		l.ID, l.Short = id.ID.String(), id.ID.Short()
	}
	peers, err := peer.LoadPeers(dir)
	if err != nil {
		return l, err
	}
	links := map[string]proto.PeerLink{}
	var st proto.ServerStatus
	if s.call(proto.MethodServerStatus, nil, &st) == nil {
		l.Running = true
		if st.Link != nil {
			l.LinkErr = st.Link.Error
			for _, pl := range st.Link.Peers {
				links[pl.ID] = pl
			}
		}
	}
	for _, p := range peers {
		e := PeerEntry{Alias: p.Alias, Name: p.Name, ID: p.ID().String(), Short: p.ID().Short(), Relay: p.Relay, Panes: p.PanesLevel()}
		if pl, ok := links[e.ID]; ok {
			e.Link = &pl
		}
		l.Peers = append(l.Peers, e)
	}
	return l, nil
}

func (s *ServerSource) PeerPopup(alias, slug string) (proto.PeerPopup, error) {
	var pp proto.PeerPopup
	if s.Caller.IsAgent() {
		return pp, errHumanOnly
	}
	// A connection of its own: the read waits for the other server,
	// and the console's other calls don't wait behind it.
	c, err := tmclient.Connect(s.Paths, false)
	if err != nil {
		return pp, err
	}
	defer c.Close()
	err = c.Call(proto.MethodPeerPopupRead, proto.PeerPopupReadParams{Alias: alias, Project: slug}, &pp)
	return pp, err
}

func (s *ServerSource) RemovePeer(alias string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	found, err := peer.Unpin(peer.DirIn(s.Paths.Home), alias)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no paired server %s (tm peer list)", alias)
	}
	var st *proto.LinkStatus
	if err := s.call(proto.MethodPeerReload, nil, &st); err != nil && !errors.Is(err, tmclient.ErrNotRunning) {
		return fmt.Errorf("removed %s, but the server didn't read the peers again (%v); tm server restart does", alias, err)
	}
	return nil
}

func (s *ServerSource) SetPeerPanes(alias, level string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	found, err := peer.SetPanes(peer.DirIn(s.Paths.Home), alias, level)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no paired server %s (tm peer list)", alias)
	}
	var st *proto.LinkStatus
	if err := s.call(proto.MethodPeerReload, nil, &st); err != nil && !errors.Is(err, tmclient.ErrNotRunning) {
		return fmt.Errorf("set, but the server didn't read the peers again (%v); tm server restart does", err)
	}
	return nil
}

func (s *ServerSource) SetPeerName(alias, name string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	h, _ := os.Hostname()
	self := peer.HostLabel(h)
	_, err := peer.SetName(peer.DirIn(s.Paths.Home), alias, name, func(n string) string { return project.PeerNameTaken(n, self) })
	if err != nil {
		return err
	}
	var st *proto.LinkStatus
	if err := s.call(proto.MethodPeerReload, nil, &st); err != nil && !errors.Is(err, tmclient.ErrNotRunning) {
		return fmt.Errorf("set, but the server didn't read the peers again (%v); tm server restart does", err)
	}
	return nil
}

func (s *ServerSource) SetPeerRelay(alias, relayText string) (string, error) {
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	r, err := peer.RelayURL(relayText)
	if err != nil {
		return "", err
	}
	found, err := peer.SetRelay(peer.DirIn(s.Paths.Home), alias, r)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("no paired server %s (tm peer list)", alias)
	}
	var st *proto.LinkStatus
	if err := s.call(proto.MethodPeerReload, nil, &st); err != nil && !errors.Is(err, tmclient.ErrNotRunning) {
		return "", fmt.Errorf("set, but the server didn't read the peers again (%v); tm server restart does", err)
	}
	return relay.PrivateWarning(r), nil
}

func (s *ServerSource) RelayUpdate(alias string) (string, error) {
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, res, err := peer.RequestRelayUpdate(ctx, peer.DirIn(s.Paths.Home), alias, nil)
	if err != nil {
		return "", err
	}
	words, ok := peer.RelayUpdateWords(relayWords(p.Relay), res)
	if !ok {
		return "", errors.New(words)
	}
	return words, nil
}

// PairEvents are what a pairing shows on the way: Waiting the code once
// it works (pairing only), Confirm the other server and the six digits,
// returning the person's answer.
type PairEvents struct {
	Waiting func(code string)
	Confirm func(PairOffer) bool
}

// PairOffer is the other server as a pairing shows it.
type PairOffer struct {
	Name, Short, Version, Check string
}

func (s *ServerSource) PairPeer(ctx context.Context, relay string, ev PairEvents) (string, error) {
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	r, err := peer.RelayURL(relay)
	if err != nil {
		return "", err
	}
	code, err := peer.NewCode(r)
	if err != nil {
		return "", err
	}
	return s.pairing(ctx, code, ev, true)
}

func (s *ServerSource) JoinPeer(ctx context.Context, text string, ev PairEvents) (string, error) {
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	code, err := peer.ParseCode(text)
	if err != nil {
		// Its words quote what was typed: not the code, which stays
		// out of messages.
		return "", errors.New("that isn't a pairing code: it is the relay, a slash and 16 digits and letters, as the other server shows it")
	}
	return s.pairing(ctx, code, ev, false)
}

// pairWait bounds a pairing: the relay's 10 minutes for a pair room,
// and a little more for the exchange once the other side joined (as tm
// peer pair).
const pairWait = 13 * time.Minute

// pairing runs tm peer pair's or join's exchange through code, then
// pins the other server as tm peer does.
func (s *ServerSource) pairing(ctx context.Context, code peer.Code, ev PairEvents, pair bool) (string, error) {
	dir := peer.DirIn(s.Paths.Home)
	id, _, err := peer.EnsureIdentity(dir)
	if err != nil {
		return "", err
	}
	host, _ := os.Hostname()
	o := peer.PairOptions{
		Name: peer.HostLabel(host), Version: version.Version,
		Confirm: func(of peer.Offer) (bool, error) {
			return ev.Confirm(PairOffer{Name: of.Name, Short: of.ID.Short(), Version: of.Version, Check: of.Check}), nil
		},
		Waiting: func() { ev.Waiting(code.String()) },
	}
	ctx, cancel := context.WithTimeout(ctx, pairWait)
	defer cancel()
	var p *peer.Paired
	if pair {
		p, err = peer.Pair(ctx, id, code, o)
	} else {
		p, err = peer.Join(ctx, id, code, o)
	}
	if err != nil {
		return "", pairWords(err, code.Relay)
	}
	got, err := peer.Pin(dir, p.Key, code.Relay, p.Name, "")
	if err != nil {
		return "", err
	}
	var st *proto.LinkStatus
	if err := s.call(proto.MethodPeerReload, nil, &st); err != nil && !errors.Is(err, tmclient.ErrNotRunning) {
		return got.Alias, fmt.Errorf("paired with %s, but the server didn't read the peers again (%v); tm server restart does", got.Alias, err)
	}
	return got.Alias, nil
}

// pairWords is a pairing's error in plain words: a relay that can't be
// reached says so, with its name.
func pairWords(err error, relayURL string) error {
	var pe *relay.PinError
	if errors.As(err, &pe) {
		return pe
	}
	var op *net.OpError
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	if errors.As(err, &op) || errors.As(err, &dns) || errors.As(err, &cert) || errors.As(err, &unknown) || errors.As(err, &host) {
		return fmt.Errorf("can't reach the relay %s: check its name, that it runs and that port 443 is open (%v)", relayWords(relayURL), err)
	}
	return err
}

func (s *ServerSource) AskShare(alias, forSlug, note string) (proto.ShareRequest, error) {
	var r proto.ShareRequest
	if s.Caller.IsAgent() {
		return r, errHumanOnly
	}
	err := s.call(proto.MethodPeerShareAsk, proto.PeerShareAskParams{Alias: alias, For: forSlug, Note: note}, &r)
	return r, refusalWords(err)
}

func (s *ServerSource) AnswerShare(id string, accept bool, slug string) (proto.ShareRequest, error) {
	var r proto.ShareRequest
	if s.Caller.IsAgent() {
		return r, errHumanOnly
	}
	defer s.touch()
	err := s.call(proto.MethodPeerShareAnswer, proto.PeerShareAnswerParams{ID: id, Accept: accept, Project: slug}, &r)
	return r, refusalWords(err)
}

// refusalWords is a method's refusal as its words, without its code: the
// server's message says it in full.
func refusalWords(err error) error {
	var pe *proto.Error
	if errors.As(err, &pe) && pe.Message != "" {
		return errors.New(pe.Message)
	}
	return err
}
