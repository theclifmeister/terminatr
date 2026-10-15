package doctor

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/update"
)

// PeerHealth is one paired server's health, the fields of a peer line
// (docs/SPEC.md §16.8); --json carries them as they are.
type PeerHealth struct {
	Alias string `json:"alias"`
	ID    string `json:"id"`
	Relay string `json:"relay"`
	State string `json:"state"`
	// Reason and Error say why an offline link is.
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
	// LatencyMS is the last ping's round trip, while up.
	LatencyMS float64 `json:"latency_ms,omitempty"`
	// LastSeen is when the link last was up as far as this server knows
	// (now while up; else when it went down); zero when it hasn't been
	// up since the server started.
	LastSeen time.Time `json:"last_seen,omitzero"`
	Version  string    `json:"version,omitempty"`
	// Skew: the peer's tm is far from this one (SkewMinor releases).
	Skew bool `json:"skew,omitempty"`
	// PanesMine is what this server lets the peer do with its panes,
	// PanesTheirs what the peer lets this one (off, view, write; empty
	// when not known).
	PanesMine   string `json:"panes_mine"`
	PanesTheirs string `json:"panes_theirs,omitempty"`
	// SharedOut counts the projects this server shares with the peer,
	// SharedIn the peer's it shares here (nil when not known).
	SharedOut int  `json:"shared_out"`
	SharedIn  *int `json:"shared_in,omitempty"`
	// Queued messages wait for the link; Undelivered were refused or
	// expired.
	Queued      int `json:"queued"`
	Undelivered int `json:"undelivered"`
}

// RelayHealth is one relay's health, the fields of a relay line.
type RelayHealth struct {
	// URL is the relay's address without its pin; Pinned says it has one.
	URL    string `json:"url"`
	Pinned bool   `json:"pinned,omitempty"`
	// Peers are the aliases that meet through it.
	Peers     []string `json:"peers"`
	Reachable bool     `json:"reachable"`
	Error     string   `json:"error,omitempty"`
	// CertDays is the days left of a CA certificate; PinMatch whether a
	// pinned relay shows the pinned key (dates aren't checked there).
	CertDays *int   `json:"cert_days,omitempty"`
	PinMatch *bool  `json:"pin_match,omitempty"`
	Version  string `json:"version,omitempty"`
	// UpdateAvailable: a release newer than the relay's is out.
	UpdateAvailable bool `json:"update_available,omitempty"`
	// AutoUpdate is "on" or "off" when the relay says; else empty.
	AutoUpdate string `json:"auto_update,omitempty"`
}

// SkewMinor is how many minor releases apart two servers' tm may be
// before doctor warns (a different major always warns).
const SkewMinor = 2

// CertWarnDays is the days left of a relay's CA certificate under which
// doctor warns.
const CertWarnDays = 14

// RelayProbe is how doctor asks a relay for /healthz.
type RelayProbe struct {
	// TLS is the base client TLS (the roots a CA certificate is checked
	// against); nil is the system's.
	TLS *tls.Config
	// Timeout bounds one probe; zero is 4 seconds.
	Timeout time.Duration
}

// Peers checks the servers this one is paired with (docs/SPEC.md §16.8):
// nothing for a server never paired; with the server stopped one line
// that counts them; with it running one line for each paired server
// (link, latency, last seen, tm version, pane levels, shared projects,
// waiting messages) and for each relay in use (healthz, certificate or
// pin, version, auto-update). A failure for an identity key or
// peers.json that can't be read, or a relay showing another key than
// the pin; a warning for an offline peer, a tm far from this one,
// undelivered messages, a relay that doesn't answer, is not up to date
// or has a certificate under CertWarnDays.
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
	out := []Check{}
	if !live.Running {
		out = append(out, Check{Group: g, Name: name, Status: OK,
			Detail: fmt.Sprintf("server id %s, %d paired; the server links them when it runs", id.ID.Short(), len(peers))})
		return append(out, relayChecks(d, live, peers, nil)...)
	}
	out = append(out, Check{Group: g, Name: name, Status: OK, Detail: fmt.Sprintf("server id %s, %d paired", id.ID.Short(), len(peers))})
	links := map[string]proto.PeerLink{}
	if live.Link != nil {
		for _, l := range live.Link.Peers {
			links[l.ID] = l
		}
	}
	mirror := map[string]proto.PeerMirror{}
	if live.Peers != nil {
		for _, m := range live.Peers.Peers {
			mirror[m.ID] = m
		}
	}
	backlog := link.Backlogs()
	shared := len(server.SharedProjects())
	var health []PeerHealth
	for _, p := range peers {
		h := PeerHealth{Alias: p.Alias, ID: p.ID().String(), Relay: relayName(p.Relay), State: "no link",
			PanesMine: proto.PanesWord(p.PanesLevel()), SharedOut: shared,
			Queued: backlog[p.Alias].Queued, Undelivered: backlog[p.Alias].Undelivered}
		if l, ok := links[h.ID]; ok {
			h.State, h.Reason, h.Error, h.LatencyMS, h.Version = l.State, l.Reason, textsafe.Line(l.Error, 200), l.RTT, textsafe.Line(l.Version, 40)
			switch {
			case l.State == proto.LinkUp:
				h.LastSeen = time.Now()
			case l.Version != "": // it said hello since the server started
				h.LastSeen = l.Since
			}
		}
		if m, ok := mirror[h.ID]; ok {
			if m.Panes != "" {
				h.PanesTheirs = proto.PanesWord(m.Panes)
			}
			n := len(m.Projects)
			h.SharedIn = &n
		}
		h.Skew = skewed(d.Version, h.Version)
		health = append(health, h)
		out = append(out, peerCheck(h))
	}
	return append(out, relayChecks(d, live, peers, health)...)
}

func relayName(raw string) string {
	r, _, err := relay.SplitPin(raw)
	if err != nil {
		return raw
	}
	return r
}

// peerCheck is the line of one paired server.
func peerCheck(h PeerHealth) Check {
	st := OK
	var parts []string
	state := h.State
	if h.Reason != "" {
		state += " (" + h.Reason + ")"
	}
	switch h.State {
	case proto.LinkUp:
		if h.LatencyMS > 0 {
			state += fmt.Sprintf(", %.0f ms", h.LatencyMS)
		}
	case proto.LinkConnecting:
	default:
		st = Warn
		switch {
		case !h.LastSeen.IsZero():
			state += ", last seen " + ago(time.Since(h.LastSeen)) + " ago"
		default:
			state += ", not seen since the server started"
		}
		if h.Error != "" {
			state += ": " + textsafe.Line(h.Error, 200)
		}
	}
	parts = append(parts, state)
	if h.Version != "" {
		v := "tm " + h.Version
		if h.Skew {
			st = Warn
			v += " (far from this tm: update the older one)"
		}
		parts = append(parts, v)
	}
	panes := "panes: they may " + h.PanesMine + " ours"
	if h.PanesTheirs != "" {
		panes += ", we may " + h.PanesTheirs + " theirs"
	}
	parts = append(parts, panes)
	sharing := fmt.Sprintf("shares %d to them", h.SharedOut)
	if h.SharedIn != nil {
		sharing += fmt.Sprintf(", %d from them", *h.SharedIn)
	}
	parts = append(parts, sharing)
	msgs := "no messages waiting"
	if h.Queued > 0 || h.Undelivered > 0 {
		msgs = fmt.Sprintf("messages: %d queued, %d undelivered", h.Queued, h.Undelivered)
	}
	if h.Undelivered > 0 {
		st = Warn
		msgs += " (see the undelivered items in the projects' inboxes)"
	}
	parts = append(parts, msgs)
	detail := strings.Join(parts, "; ")
	if st == Warn && (h.Reason == proto.LinkReasonRefused || h.Reason == proto.LinkReasonVersion || h.Reason == proto.LinkReasonRelay) {
		detail += "; tm peer list, and docs/OPERATIONS.md, The servers' side"
	}
	return Check{Group: "peers", Name: "peer " + h.Alias, Status: st, Detail: detail, Peer: &h}
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

// skewed says the tm releases a and b are SkewMinor minor releases or
// more apart, or of different majors; false where either isn't a
// release.
func skewed(a, b string) bool {
	x, ok1 := releaseOf(a)
	y, ok2 := releaseOf(b)
	if !ok1 || !ok2 {
		return false
	}
	if x[0] != y[0] {
		return true
	}
	diff := x[1] - y[1]
	return diff >= SkewMinor || -diff >= SkewMinor
}

func releaseOf(v string) (n [2]int, ok bool) {
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	var patch int
	if c, err := fmt.Sscanf(v, "%d.%d.%d", &n[0], &n[1], &patch); err != nil || c != 3 {
		return n, false
	}
	return n, true
}

// relayChecks is a line for each relay the peers use, in the order the
// first peer using it comes; none without d.Relay.
func relayChecks(d Deps, live Live, peers []peer.Peer, health []PeerHealth) []Check {
	if d.Relay == nil {
		return nil
	}
	var urls []string
	by := map[string][]peer.Peer{}
	for _, p := range peers {
		if !slices.Contains(urls, p.Relay) {
			urls = append(urls, p.Relay)
		}
		by[p.Relay] = append(by[p.Relay], p)
	}
	var out []Check
	for _, raw := range urls {
		h := probeRelay(d.Relay, raw)
		var seen string
		for _, p := range by[raw] {
			h.Peers = append(h.Peers, p.Alias)
			if live.Link != nil && seen == "" {
				for _, l := range live.Link.Peers {
					if l.ID == p.ID().String() {
						seen = textsafe.Line(l.RelayVersion, 40)
					}
				}
			}
		}
		if h.Version == "" {
			h.Version = seen
		}
		if h.Version != "" {
			latest := d.Version
			if d.Latest != nil {
				if l, err := d.Latest(); err == nil && l != "" {
					latest = l
				}
			}
			if newer, ok := update.Newer(latest, h.Version); ok && newer {
				h.UpdateAvailable = true
			}
		}
		out = append(out, relayCheck(h))
	}
	return out
}

// relayCheck is the line of one relay.
func relayCheck(h RelayHealth) Check {
	c := Check{Group: "peers", Name: "relay " + h.URL, Status: OK, Relay: &h}
	if !h.Reachable {
		c.Status = Warn
		c.Detail = "not reachable: " + textsafe.Line(h.Error, 200)
		if h.PinMatch != nil && !*h.PinMatch {
			c.Status = Fail
		}
		return c
	}
	parts := []string{"reachable"}
	switch {
	case h.PinMatch != nil:
		parts = append(parts, "pin matches")
	case h.CertDays != nil && *h.CertDays < 0:
		c.Status = Warn
		parts = append(parts, "certificate expired")
	case h.CertDays != nil && *h.CertDays < CertWarnDays:
		c.Status = Warn
		parts = append(parts, fmt.Sprintf("certificate expires in %d days", *h.CertDays))
	case h.CertDays != nil:
		parts = append(parts, fmt.Sprintf("certificate good for %d days", *h.CertDays))
	}
	if h.Version != "" {
		v := "tm " + h.Version
		if h.UpdateAvailable {
			c.Status = max(c.Status, Warn)
			v += ", update available"
		}
		parts = append(parts, v)
	}
	if h.AutoUpdate != "" {
		parts = append(parts, "auto-update "+h.AutoUpdate)
	}
	c.Detail = strings.Join(parts, "; ")
	if h.UpdateAvailable {
		c.Detail += " (tm peer relay-update ALIAS)"
	}
	return c
}

// probeRelay asks the relay at raw (a wss:// URL, pin and all) for
// /healthz and reads its certificate: days left of a CA's, the pinned
// key's match for a pinned relay.
func probeRelay(rp *RelayProbe, raw string) RelayHealth {
	h := RelayHealth{URL: relayName(raw)}
	base, pin, err := relay.SplitPin(raw)
	if err != nil {
		h.Error = err.Error()
		return h
	}
	h.Pinned = pin != ""
	u, err := url.Parse(base)
	if err != nil {
		h.Error = err.Error()
		return h
	}
	scheme := "https"
	if u.Scheme == "ws" {
		scheme = "http"
	}
	hz := scheme + "://" + u.Host + "/healthz"
	cfg := rp.TLS
	if pin != "" {
		cfg = relay.PinnedTLS(rp.TLS, u.Host, pin)
	}
	timeout := rp.Timeout
	if timeout == 0 {
		timeout = 4 * time.Second
	}
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true, Proxy: nil}}
	defer client.CloseIdleConnections()
	resp, err := client.Get(hz)
	if err != nil {
		var pe *relay.PinError
		if errors.As(err, &pe) {
			f := false
			h.PinMatch = &f
			h.Error = pe.Error()
			return h
		}
		h.Error = errors.Unwrap(err).Error()
		return h
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		h.Error = "/healthz answered " + resp.Status
		return h
	}
	h.Reachable = true
	h.AutoUpdate = resp.Header.Get("Tm-Auto-Update")
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		if pin != "" {
			t := true
			h.PinMatch = &t
		} else {
			days := int(time.Until(resp.TLS.PeerCertificates[0].NotAfter).Hours() / 24)
			h.CertDays = &days
		}
	}
	return h
}
