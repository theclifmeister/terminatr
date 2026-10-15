package cli

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/version"
)

const peerUsage = `usage: tm peer pair [--relay URL] [--as ALIAS]
       tm peer join CODE [--as ALIAS]
       tm peer list [--json]
       tm peer remove ALIAS
       tm peer rotate [--yes]
       tm peer panes ALIAS off|view|write
       tm peer relay ALIAS ADDRESS
       tm peer relay-update ALIAS`

func init() {
	commands["peer"] = runPeer
	checked["peer"] = true
}

// pairWait bounds tm peer pair's wait: the relay's 10 minutes for a
// pair room, and a little more for the exchange once the other joined.
const pairWait = 13 * time.Minute

// Variables for tests: the relay's outer TLS (nil trusts the system's
// CAs), the name this server gives the other side, and how a running
// server is told and asked about the peers.
var (
	pairRelayTLS = func() *tls.Config { return nil }
	pairName     = func() string { h, _ := os.Hostname(); return peer.HostLabel(h) }
	reloadPeers  = serverReloadPeers
	peerLinks    = serverPeerLinks
)

// runPeer is tm peer (docs/SPEC.md §16.8): pairing this server with
// another through a relay, and the paired servers. All of it is the
// human's.
func runPeer(e *Env, args []string) error {
	if len(args) == 0 {
		return usagef("%s", peerUsage)
	}
	f := newFlags()
	relayFlag, as := f.String("relay"), f.String("as")
	asJSON, yes := f.Bool("json"), f.Bool("yes")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	allowed := map[string][]string{"pair": {"relay", "as"}, "join": {"as"}, "list": {"json"}, "rotate": {"yes"}}[args[0]]
	for _, name := range []string{"relay", "as", "json", "yes"} {
		if f.IsSet(name) && !slices.Contains(allowed, name) {
			return usagef("tm peer %s takes no --%s\n%s", args[0], name, peerUsage)
		}
	}
	npos := map[string]int{"pair": 0, "join": 1, "list": 0, "remove": 1, "rotate": 0, "panes": 2, "relay": 2, "relay-update": 1}
	if n, ok := npos[args[0]]; !ok || len(pos) != n {
		return usagef("%s", peerUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "pairing servers is the user's, in their own terminal"}
	}
	if *as != "" {
		if err := peer.CheckAlias(*as); err != nil {
			return usagef("--as: %v", err)
		}
	}
	dir, err := peer.Dir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "pair":
		return e.peerPair(dir, *relayFlag, *as)
	case "join":
		code, err := peer.ParseCode(pos[0])
		if err != nil {
			return usagef("%v", err)
		}
		return e.peerJoin(dir, code, *as)
	case "list":
		return e.peerList(dir, *asJSON)
	case "remove":
		return e.peerRemove(dir, pos[0])
	case "panes":
		return e.peerPanes(dir, pos[0], pos[1])
	case "relay":
		return e.peerRelay(dir, pos[0], pos[1])
	case "relay-update":
		return e.peerRelayUpdate(dir, pos[0])
	default:
		return e.peerRotate(dir, *yes)
	}
}

// pairRelay is the relay to pair through: --relay, else the one every
// pinned peer uses.
func pairRelay(dir, flag string) (string, error) {
	if flag != "" {
		r, err := peer.RelayURL(flag)
		if err != nil {
			return "", usagef("--relay: %v", err)
		}
		return r, nil
	}
	peers, err := peer.LoadPeers(dir)
	if err != nil {
		return "", err
	}
	relays := map[string]bool{}
	for _, p := range peers {
		relays[p.Relay] = true
	}
	if len(relays) == 1 {
		return peers[0].Relay, nil
	}
	return "", usagef("--relay URL: the relay both servers dial (docs/OPERATIONS.md, Running a relay)\n%s", peerUsage)
}

// pairIdentity is this server's identity, made now when it has none.
func (e *Env) pairIdentity(dir string) (*peer.Identity, error) {
	id, created, err := peer.EnsureIdentity(dir)
	if err != nil {
		return nil, err
	}
	if created {
		fmt.Fprintf(e.Stdout, "Made this server's identity key: server id %s\n", id.ID.Short())
	}
	return id, nil
}

func (e *Env) pairOptions() peer.PairOptions {
	in := bufio.NewReader(e.Stdin)
	return peer.PairOptions{
		Name: pairName(), Version: version.Version, RelayTLS: pairRelayTLS(),
		Confirm: func(o peer.Offer) (bool, error) {
			name := o.Name
			if name == "" {
				name = "(no name)"
			}
			fmt.Fprintf(e.Stdout, "\nThe other server: %q, server id %s, tm %s\n", name, o.ID.Short(), o.Version)
			fmt.Fprintf(e.Stdout, "Check: %s\n\n", o.Check)
			fmt.Fprint(e.Stdout, "Pair only if the other side shows the same six digits. Do they match? [y/N] ")
			line, err := in.ReadString('\n')
			if err != nil && line == "" {
				fmt.Fprintln(e.Stdout)
				return false, nil
			}
			a := strings.ToLower(strings.TrimSpace(line))
			return a == "y" || a == "yes", nil
		},
	}
}

func (e *Env) peerPair(dir, relayFlag, as string) error {
	relayURL, err := pairRelay(dir, relayFlag)
	if err != nil {
		return err
	}
	id, err := e.pairIdentity(dir)
	if err != nil {
		return err
	}
	code, err := peer.NewCode(relayURL)
	if err != nil {
		return err
	}
	e.warnPrivate(relayURL)
	o := e.pairOptions()
	o.Waiting = func() {
		fmt.Fprintf(e.Stdout, "Pairing code (works once, for 10 minutes):\n\n    %s\n\n", code)
		fmt.Fprintf(e.Stdout, "On the other server: tm peer join %s\nPass it on yourself (a chat, a call), never through the relay.\n", code)
		if code.Pinned() {
			fmt.Fprintf(e.Stdout, "The code holds the relay's pin: the other server needs tm %s or later to read it.\n", version.Version)
		}
		fmt.Fprintln(e.Stdout, "Waiting…")
	}
	ctx, stop := proc.UntilStopped(context.Background())
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, pairWait)
	defer cancel()
	p, err := peer.Pair(ctx, id, code, o)
	if err != nil {
		return pairFailed(err)
	}
	return e.pin(dir, p, relayURL, as)
}

func (e *Env) peerJoin(dir string, code peer.Code, as string) error {
	id, err := e.pairIdentity(dir)
	if err != nil {
		return err
	}
	ctx, stop := proc.UntilStopped(context.Background())
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, pairWait)
	defer cancel()
	e.warnPrivate(code.Relay)
	fmt.Fprintf(e.Stdout, "Joining through %s…\n", relayHost(code.Relay))
	p, err := peer.Join(ctx, id, code, e.pairOptions())
	if err != nil {
		return pairFailed(err)
	}
	return e.pin(dir, p, code.Relay, as)
}

// pairFailed is a pairing's error as tm peer reports it: an outcome
// people caused (a no, a wrong code) is a refusal, not an I/O error.
func pairFailed(err error) error {
	for _, r := range []struct {
		err  error
		code string
	}{
		{peer.ErrDeclined, "declined"}, {peer.ErrPeerNo, "declined"}, {peer.ErrBadCode, "bad-code"},
		{peer.ErrNoPairer, "bad-code"}, {peer.ErrOwnKey, "own-server"},
	} {
		if errors.Is(err, r.err) {
			return &project.Error{Code: r.code, Msg: err.Error()}
		}
	}
	return err
}

// pin keeps the paired server and tells a running server.
func (e *Env) pin(dir string, p *peer.Paired, relayURL, as string) error {
	got, err := peer.Pin(dir, p.Key, relayURL, p.Name, as)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "Paired with %s (server id %s) through %s.\n", got.Alias, got.ID().Short(), relayHost(relayURL))
	e.reload()
	return nil
}

// reload has a running server read the peers again.
func (e *Env) reload() {
	switch err := reloadPeers(); {
	case errors.Is(err, server.ErrNotRunning):
		fmt.Fprintln(e.Stdout, "The server isn't running; it reads the peers when it starts.")
	case err != nil:
		fmt.Fprintf(e.Stderr, "tm peer: the server didn't read the peers again (%v); tm server restart does\n", err)
	}
}

func serverReloadPeers() error {
	c, _, err := connect(false)
	if err != nil {
		return err
	}
	defer c.Close()
	var st *proto.LinkStatus
	return c.Call(proto.MethodPeerReload, nil, &st)
}

// serverPeerLinks is a running server's link status; ok false when no
// server answers.
func serverPeerLinks() (st *proto.LinkStatus, ok bool) {
	c, _, err := connect(false)
	if err != nil {
		return nil, false
	}
	defer c.Close()
	var s proto.ServerStatus
	if c.Call(proto.MethodServerStatus, nil, &s) != nil {
		return nil, false
	}
	return s.Link, true
}

// relayHost is the relay as people read it: its host for wss://, and
// "(pinned)" for one taken by its key's pin.
func relayHost(r string) string {
	if u, err := url.Parse(r); err == nil && u.Scheme == "wss" && u.Path == "" {
		if u.Fragment != "" {
			return u.Host + " (pinned)"
		}
		return u.Host
	}
	return r
}

// warnPrivate says when the relay is on a private address.
func (e *Env) warnPrivate(relayURL string) {
	if w := relay.PrivateWarning(relayURL); w != "" {
		fmt.Fprintf(e.Stdout, "Note: %s.\n", w)
	}
}

// peerRelay gives the paired server alias another relay: the relay
// moved, or was set up again with a new key (docs/SPEC.md §16.2).
func (e *Env) peerRelay(dir, alias, address string) error {
	r, err := peer.RelayURL(address)
	if err != nil {
		return usagef("%v", err)
	}
	found, err := peer.SetRelay(dir, alias, r)
	if err != nil {
		return err
	}
	if !found {
		return &project.Error{Code: "unknown-peer", Msg: fmt.Sprintf("no peer %q (tm peer list)", alias)}
	}
	fmt.Fprintf(e.Stdout, "%s's link now goes through %s. Set the same relay on %s for this server too (tm peer relay ALIAS %s there).\n", alias, relayHost(r), alias, r)
	e.warnPrivate(r)
	e.reload()
	return nil
}

// peerRelayUpdate asks the relay of the paired server alias to update
// itself to the latest official release (docs/SPEC.md §16.2).
func (e *Env) peerRelayUpdate(dir, alias string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, res, err := peer.RequestRelayUpdate(ctx, dir, alias, pairRelayTLS())
	var unknown *peer.UnknownPeerError
	switch {
	case errors.As(err, &unknown):
		return &project.Error{Code: "unknown-peer", Msg: err.Error()}
	case err != nil && p.Alias == "":
		return err
	case err != nil:
		return fmt.Errorf("the relay %s: %w", relayHost(p.Relay), err)
	}
	words, ok := peer.RelayUpdateWords(relayHost(p.Relay), res)
	if !ok {
		return &project.Error{Code: "relay-update-" + res.Result, Msg: words}
	}
	if res.Version != "" {
		words += " (it runs tm " + textsafe.Line(strings.TrimPrefix(res.Version, "v"), 40) + " now)"
	}
	fmt.Fprintln(e.Stdout, strings.ToUpper(words[:1])+words[1:]+".")
	return nil
}

// peerEntry is one paired server in tm peer list --json.
type peerEntry struct {
	Alias string          `json:"alias"`
	ID    string          `json:"id"`
	Short string          `json:"short"`
	Relay string          `json:"relay"`
	Link  *proto.PeerLink `json:"link,omitempty"`
	// Panes is this server's pane switch for the peer (docs/SPEC.md
	// §16.12), as the user says it: off, view or write.
	Panes string `json:"panes"`
}

func (e *Env) peerList(dir string, asJSON bool) error {
	id, err := peer.LoadIdentity(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	peers, err := peer.LoadPeers(dir)
	if err != nil {
		return err
	}
	st, running := peerLinks()
	links := map[string]proto.PeerLink{}
	if st != nil {
		for _, l := range st.Peers {
			links[l.ID] = l
		}
	}
	entries := []peerEntry{}
	for _, p := range peers {
		pe := peerEntry{Alias: p.Alias, ID: p.ID().String(), Short: p.ID().Short(), Relay: p.Relay, Panes: proto.PanesWord(p.PanesLevel())}
		if l, ok := links[pe.ID]; ok {
			pe.Link = &l
		}
		entries = append(entries, pe)
	}
	if asJSON {
		out := map[string]any{"peers": entries}
		if id != nil {
			out["id"], out["short"] = id.ID.String(), id.ID.Short()
		}
		return e.printJSON(out)
	}
	if id == nil {
		fmt.Fprintln(e.Stdout, "This server isn't paired: tm peer pair --relay URL, or tm peer join CODE")
		return nil
	}
	fmt.Fprintf(e.Stdout, "server id %s (%s)\n", id.ID.Short(), id.ID)
	if len(entries) == 0 {
		fmt.Fprintln(e.Stdout, "no peers: tm peer pair, or tm peer join CODE")
		return nil
	}
	now := time.Now()
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	for _, pe := range entries {
		state := "link not running"
		switch {
		case pe.Link != nil:
			state = linkWords(*pe.Link, now)
		case running && st != nil && st.Error != "":
			state = "not read: " + textsafe.Line(st.Error, 200)
		case running:
			state = "no link yet"
		}
		if pe.Panes != proto.PanesOff {
			state += "; panes: " + pe.Panes
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", pe.Alias, pe.Short, relayHost(pe.Relay), state)
	}
	return tw.Flush()
}

func (e *Env) peerRemove(dir, alias string) error {
	found, err := peer.Unpin(dir, alias)
	if err != nil {
		return err
	}
	if !found {
		return &project.Error{Code: "unknown-peer", Msg: fmt.Sprintf("no peer %q (tm peer list)", alias)}
	}
	fmt.Fprintf(e.Stdout, "Removed %s; its link closes. Remove this server on the other side too.\n", alias)
	e.reload()
	return nil
}

// peerPanes sets what the paired server alias may do with this server's
// panes (docs/SPEC.md §16.12): off, view (watch the shared projects'
// coordinator and thread panes) or write (watch and type into them;
// type is its older name, still taken).
func (e *Env) peerPanes(dir, alias, level string) error {
	if !proto.ValidPanes(level) {
		return usagef("tm peer panes ALIAS off|view|write: %q isn't one", level)
	}
	found, err := peer.SetPanes(dir, alias, level)
	if err != nil {
		return err
	}
	if !found {
		return &project.Error{Code: "unknown-peer", Msg: fmt.Sprintf("no peer %q (tm peer list)", alias)}
	}
	switch level {
	case proto.PanesOff:
		fmt.Fprintf(e.Stdout, "Panes off for %s: it can't open this server's panes, and those it has open close.\n", alias)
	case proto.PanesView:
		fmt.Fprintf(e.Stdout, "Panes view for %s: it can watch the coordinator and thread panes of the projects this server shares, without typing.\n", alias)
	default:
		fmt.Fprintf(e.Stdout, "Panes write for %s: it can watch and type into the coordinator and thread panes of the projects this server shares; the first keys into a thread tell its coordinator.\n", alias)
	}
	e.reload()
	return nil
}

func (e *Env) peerRotate(dir string, yes bool) error {
	peers, err := peer.LoadPeers(dir)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Fprintf(e.Stdout, "A new identity key: every paired server (%d) refuses this server until the two pair again. Rotate? [y/N] ", len(peers))
		line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(e.Stdout, "nothing done")
			return &exitError{code: ExitRefused}
		}
	}
	id, err := peer.RotateIdentity(dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "New identity key: server id %s\n", id.ID.Short())
	for _, p := range peers {
		fmt.Fprintf(e.Stdout, "Pair again with %s: tm peer pair --as %s (and tm peer join on it)\n", p.Alias, p.Alias)
	}
	e.reload()
	return nil
}
