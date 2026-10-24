package cli

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

const networkUsage = `usage: tm network [--json]   (this server, the paired servers, what each shares, the links and what needs you)`

func init() {
	commands["network"] = runNetwork
	checked["network"] = true
}

// peerMirror is a running server's mirror of the paired servers
// (peer.list); ok false when no server answers. A variable for tests.
var peerMirror = serverPeerMirror

func serverPeerMirror() (*proto.PeerList, bool) {
	c, _, err := connect(false)
	if err != nil {
		return nil, false
	}
	defer c.Close()
	var l proto.PeerList
	if c.Call(proto.MethodPeerList, nil, &l) != nil {
		return nil, false
	}
	return &l, true
}

// The tree tm network prints and --json answers: the Network popup's,
// read-only (docs/SPEC.md §10, §16.8).
type (
	networkTree struct {
		Server networkHere   `json:"server"`
		Peers  []networkPeer `json:"peers"`
		Needs  networkNeeds  `json:"needs"`
		// Running: a server answered; without one the connections and
		// the paired servers' projects aren't known.
		Running bool `json:"running"`
	}
	networkHere struct {
		Name string `json:"name"`
		// ID and Short are this server's id, "" before it has an
		// identity key.
		ID       string           `json:"id,omitempty"`
		Short    string           `json:"short,omitempty"`
		Projects []networkProject `json:"projects"`
	}
	// networkProject is one of this server's active projects: whether
	// it shares it with the paired servers, and its links.
	networkProject struct {
		Slug   string        `json:"slug"`
		Shared bool          `json:"shared"`
		Links  []networkLink `json:"links"`
	}
	// networkLink is a link of a project here: each side's entry (the
	// other side's as far as known: a paired server's project's from
	// the messages and requests), its health, and what it needs.
	networkLink struct {
		Peer   string       `json:"peer"`
		Shown  string       `json:"shown,omitempty"`
		Remote bool         `json:"remote,omitempty"`
		Arrow  string       `json:"arrow"`
		Mine   *config.Link `json:"mine,omitempty"`
		Theirs *config.Link `json:"theirs,omitempty"`
		// There is the other side in words; Health the link's state.
		There  string `json:"there"`
		Health string `json:"health"`
		Paused bool   `json:"paused,omitempty"`
		Unread int    `json:"unread,omitempty"`
		Queued int    `json:"queued,omitempty"`
		// Held are this side's messages waiting for the user's OK,
		// Files the files offered here that wait for them, by id.
		Held  []string        `json:"held,omitempty"`
		Files []link.Transfer `json:"files,omitempty"`
		Asks  *link.Request   `json:"asks,omitempty"`
	}
	// networkPeer is one paired server: its connection, the panes
	// switch each side gives the other, the projects it shares with
	// this one, and the share requests between the two.
	networkPeer struct {
		Alias      string          `json:"alias"`
		Name       string          `json:"name,omitempty"`
		ID         string          `json:"id"`
		Short      string          `json:"short"`
		Relay      string          `json:"relay"`
		Connection *proto.PeerLink `json:"connection,omitempty"`
		Version    string          `json:"version,omitempty"`
		// Panes is this server's switch for it; TheirPanes its switch
		// for this one, as it last said ("" unknown).
		Panes      string               `json:"panes"`
		TheirPanes string               `json:"their_panes,omitempty"`
		Listed     bool                 `json:"listed,omitempty"`
		Projects   []networkTheirs      `json:"projects"`
		Shares     []proto.ShareRequest `json:"shares,omitempty"`
	}
	// networkTheirs is a project a paired server shares: from the
	// mirror (stale while it is offline), and this server's projects
	// linked to it.
	networkTheirs struct {
		Project     string   `json:"project"`
		Stale       bool     `json:"stale,omitempty"`
		Paused      bool     `json:"paused,omitempty"`
		NeedsYou    int      `json:"needs_you"`
		Threads     int      `json:"threads"`
		Coordinator string   `json:"coordinator,omitempty"`
		Linked      []string `json:"linked,omitempty"`
	}
	// networkNeeds counts what needs the user across every project
	// and paired server.
	networkNeeds struct {
		Total         int `json:"total"`
		Held          int `json:"held"`
		Files         int `json:"files"`
		LinkRequests  int `json:"link_requests"`
		ShareRequests int `json:"share_requests"`
		Paused        int `json:"paused"`
	}
)

// runNetwork is tm network: the human's, like tm peer list.
func runNetwork(e *Env, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usagef("%s", networkUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "the network of servers and links is the user's, in their own terminal"}
	}
	t, err := readNetwork()
	if err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(t)
	}
	e.printNetwork(t, time.Now())
	return nil
}

// readNetwork reads the tree: the identity and the peers from disk,
// the connections and the mirror from a running server, the projects
// and their links from their files.
func readNetwork() (*networkTree, error) {
	dir, err := peer.Dir()
	if err != nil {
		return nil, err
	}
	id, err := peer.LoadIdentity(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	peers, err := peer.LoadPeers(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	t := &networkTree{Server: networkHere{Name: peer.HostLabel(host), Projects: []networkProject{}}, Peers: []networkPeer{}}
	if id != nil {
		t.Server.ID, t.Server.Short = id.ID.String(), id.ID.Short()
	}
	st, running := peerLinks()
	conns := map[string]proto.PeerLink{}
	if st != nil {
		for _, l := range st.Peers {
			conns[l.ID] = l
		}
	}
	mirrors := map[string]*proto.PeerMirror{}
	if l, ok := peerMirror(); ok {
		running = true
		for i := range l.Peers {
			mirrors[l.Peers[i].ID] = &l.Peers[i]
		}
	}
	t.Running = running
	byAlias := map[string]*proto.PeerMirror{}
	for _, p := range peers {
		if pm := mirrors[p.ID().String()]; pm != nil {
			byAlias[p.Alias] = pm
		}
	}

	list, _ := project.List()
	for _, sum := range list {
		if sum.Error != "" || sum.Safety != nil && !sum.Safety.Active {
			continue
		}
		p, err := project.Open(sum.Slug)
		if err != nil {
			continue
		}
		np := networkProject{Slug: p.Slug, Shared: sum.Safety != nil && sum.Safety.Share, Links: []networkLink{}}
		for _, r := range link.Rows(cfg, p) {
			if !r.HasMine && !r.HasTheirs && r.Asks == nil {
				continue
			}
			nl := readLink(p, r, byAlias, running, peers)
			np.Links = append(np.Links, nl)
			t.Needs.Held += len(nl.Held)
			t.Needs.Files += len(nl.Files)
			if nl.Asks != nil {
				t.Needs.LinkRequests++
			}
			if nl.Paused {
				t.Needs.Paused++
			}
		}
		t.Server.Projects = append(t.Server.Projects, np)
	}

	shares := map[string][]proto.ShareRequest{}
	for _, r := range link.ShareRequests() {
		if r.Pending() {
			shares[r.Peer] = append(shares[r.Peer], r)
		}
	}
	for _, p := range peers {
		np := networkPeer{Alias: p.Alias, Name: p.Name, ID: p.ID().String(), Short: p.ID().Short(), Relay: p.Relay,
			Panes: proto.PanesWord(p.PanesLevel()), Projects: []networkTheirs{}, Shares: shares[p.Alias]}
		if c, ok := conns[np.ID]; ok {
			np.Connection = &c
			np.Version = c.Version
		}
		if pm := byAlias[p.Alias]; pm != nil {
			np.Version = cmp.Or(pm.Version, np.Version)
			if pm.Pane {
				np.TheirPanes = proto.PanesWord(cmp.Or(pm.Panes, proto.PanesOff))
			}
			np.Listed = pm.Listed
			for _, mp := range pm.Projects {
				np.Projects = append(np.Projects, theirProject(t.Server.Projects, p.Alias, mp))
			}
		}
		np.Version = textsafe.Line(strings.TrimPrefix(np.Version, "v"), 40)
		for _, r := range np.Shares {
			if !r.Out {
				t.Needs.ShareRequests++
			}
		}
		t.Peers = append(t.Peers, np)
	}
	n := t.Needs
	t.Needs.Total = n.Held + n.Files + n.LinkRequests + n.ShareRequests + n.Paused
	return t, nil
}

// readLink is link row r of project p as tm network has it.
func readLink(p *project.Project, r link.Row, mirrors map[string]*proto.PeerMirror, running bool, peers []peer.Peer) networkLink {
	nl := networkLink{Peer: r.Peer, Shown: r.Shown, Remote: r.Remote, Arrow: r.Arrow(), Paused: r.Paused,
		Unread: r.Unread, Queued: r.Queued, Asks: r.Asks}
	if r.HasMine {
		mine := r.Mine
		nl.Mine = &mine
	}
	if r.HasTheirs {
		theirs := r.Theirs
		nl.Theirs = &theirs
	}
	var msgs []*link.Message
	if r.Remote || r.Held > 0 {
		msgs, _ = link.Log(p, r.Peer)
	}
	for _, m := range msgs {
		if m.Out && m.State == link.StateHeld {
			nl.Held = append(nl.Held, m.ID)
		}
	}
	if r.FilesWaiting > 0 {
		for _, f := range link.Transfers(p, r.Peer) {
			if !f.Out && f.State == link.FileOffered {
				nl.Files = append(nl.Files, f)
			}
		}
	}
	nl.There, nl.Health = linkHealth(r, link.TheirSide(msgs), mirrors, running, peers)
	return nl
}

// linkHealth is a link's other side in words and its state: for a link
// here both entries are known; for a paired server's project its side
// as far as its messages and requests tell, and why nothing crosses.
func linkHealth(r link.Row, back link.Back, mirrors map[string]*proto.PeerMirror, running bool, peers []peer.Peer) (there, health string) {
	alias, other, remote := config.Remote(r.Peer)
	if !remote {
		there = cliSide(r.Theirs, r.HasTheirs)
		health = r.Words()
		return there, health
	}
	there = back.Words()
	if q := r.Asked; q != nil && (q.Pending() || q.State == link.ReqDeclined || back == link.Back{}) {
		there = q.Words()
	}
	name := peer.HomeNames().Show(alias)
	pm := mirrors[alias]
	switch {
	case r.Paused:
		health = "paused"
	case !slices.ContainsFunc(peers, func(p peer.Peer) bool { return p.Alias == alias }):
		health = name + " isn't paired"
	case pm == nil && !running:
		health = r.Words() + " (the server isn't running)"
	case pm == nil:
		health = r.Words()
	case r.HasMine && pm.Listed && !slices.ContainsFunc(pm.Projects, func(p proto.PeerMirrorProject) bool { return p.Project == other }):
		health = "not shared any more"
	case pm.State != proto.LinkUp && r.Queued > 0:
		health = fmt.Sprintf("%s offline · %d queued", name, r.Queued)
	case pm.State != proto.LinkUp:
		health = name + " offline"
	case r.Asked != nil && r.Asked.Pending():
		health = "waiting on " + name + "'s user"
	default:
		health = r.Words()
	}
	return there, health
}

// cliSide is a link entry's settings as the CLI says them: "both ask ·
// files ask", "no entry".
func cliSide(l config.Link, has bool) string {
	if !has {
		return "no entry"
	}
	return linkAllowWords(l.Allow, l.Ask) + " · files " + l.FilesMode()
}

// theirProject is a paired server's shared project mp: its coordinator,
// and the projects here linked to it (from projects' links).
func theirProject(projects []networkProject, alias string, mp proto.PeerMirrorProject) networkTheirs {
	nt := networkTheirs{Project: mp.Project, Stale: mp.Stale, Paused: mp.Paused, NeedsYou: mp.NeedsYou, Threads: mp.Threads}
	if i := slices.IndexFunc(mp.Sessions, func(s proto.PeerSession) bool { return s.Role == proto.RoleCoordinator }); i >= 0 {
		nt.Coordinator = cmp.Or(mp.Sessions[i].State, "running")
	} else if mp.Watch != nil {
		nt.Coordinator = "not running"
	}
	key := alias + "/" + mp.Project
	for _, p := range projects {
		for _, l := range p.Links {
			if l.Peer == key && l.Mine != nil {
				nt.Linked = append(nt.Linked, p.Slug)
			}
		}
	}
	return nt
}

// printNetwork prints the tree as text.
func (e *Env) printNetwork(t *networkTree, now time.Time) {
	w := e.Stdout
	head := t.Server.Name + " · this server"
	if t.Server.Short != "" {
		head += " · id " + t.Server.Short
	}
	shared := 0
	for _, p := range t.Server.Projects {
		if p.Shared {
			shared++
		}
	}
	fmt.Fprintf(w, "%s · shares %d of %d projects with every paired server\n", head, shared, len(t.Server.Projects))
	if len(t.Server.Projects) == 0 {
		fmt.Fprintln(w, "  no active project here")
	}
	width := 0
	for _, p := range t.Server.Projects {
		width = max(width, len(p.Slug))
	}
	for _, p := range t.Server.Projects {
		box := "not shared"
		if p.Shared {
			box = "shared"
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, p.Slug, box)
		lw := 0
		for _, l := range p.Links {
			if l.Mine != nil || l.Theirs != nil {
				lw = max(lw, len(cmp.Or(l.Shown, l.Peer)))
			}
		}
		for _, l := range p.Links {
			if l.Mine == nil && l.Theirs == nil {
				fmt.Fprintf(w, "    ⚑ %s asks to link (%s there, %s): tm link accept|decline %s --project %s\n", link.Shown(l.Peer), l.Asks.Allow, l.Asks.ID, l.Asks.ID, p.Slug)
				continue
			}
			mine := "no entry"
			if l.Mine != nil {
				mine = cliSide(*l.Mine, true)
			}
			fmt.Fprintf(w, "    %s %-*s  here %s · there %s · %s%s\n", l.Arrow, lw, cmp.Or(l.Shown, l.Peer), mine, l.There, l.Health, networkCounts(l))
			for _, id := range l.Held {
				fmt.Fprintf(w, "      ⚑ %s waits for your OK: tm link approve|drop %s --project %s\n", id, id, p.Slug)
			}
			for _, f := range l.Files {
				fmt.Fprintf(w, "      ⚑ %s (%s) offered with %s: tm link accept|decline %s --project %s\n", textsafe.Line(f.Name, 80), link.MB(f.Size), f.Message, f.ID, p.Slug)
			}
			if l.Asks != nil {
				fmt.Fprintf(w, "      ⚑ asks to link (%s there, %s): tm link accept|decline %s --project %s\n", l.Asks.Allow, l.Asks.ID, l.Asks.ID, p.Slug)
			}
			if l.Paused {
				fmt.Fprintf(w, "      ⚑ paused: too many messages without you; tm link resume %s --project %s\n", l.Peer, p.Slug)
			}
		}
	}
	if len(t.Peers) == 0 {
		fmt.Fprintln(w, "\nno paired server: tm peer pair --relay URL, or tm peer join CODE")
	}
	for _, pe := range t.Peers {
		fmt.Fprintln(w)
		conn := "the server isn't running"
		switch {
		case pe.Connection != nil:
			conn = linkWords(*pe.Connection, now)
		case t.Running:
			conn = "no link yet"
		}
		if pe.Version != "" && !strings.Contains(conn, "tm ") {
			conn += " · tm " + pe.Version
		}
		panes := "panes: you give it " + pe.Panes
		if pe.TheirPanes != "" {
			panes += ", it gives you " + pe.TheirPanes
		}
		fmt.Fprintf(w, "%s · %s · %s · %s\n", peer.Both(pe.Name, pe.Alias), pe.Short, conn, panes)
		switch {
		case len(pe.Projects) == 0 && !t.Running:
		case len(pe.Projects) == 0:
			fmt.Fprintln(w, "  shares nothing with you yet: tm peer ask-share "+pe.Alias+" asks its user to share one")
		}
		width := 0
		for _, p := range pe.Projects {
			width = max(width, len(p.Project))
		}
		for _, p := range pe.Projects {
			bits := []string{"shared with you"}
			if p.Stale {
				bits[0] += ", stale"
			}
			if p.Coordinator != "" {
				bits = append(bits, "coordinator "+textsafe.Line(p.Coordinator, 40))
			}
			if p.Paused {
				bits = append(bits, "paused")
			}
			if len(p.Linked) > 0 {
				bits = append(bits, "linked from "+strings.Join(p.Linked, ", "))
			}
			fmt.Fprintf(w, "  %-*s  %s\n", width, textsafe.Line(p.Project, 80), strings.Join(bits, " · "))
		}
		for _, r := range pe.Shares {
			if r.Out {
				fmt.Fprintf(w, "  %s · waiting on %s's user\n", link.AskedWords(&r), peer.HomeNames().Show(pe.Alias))
				continue
			}
			fmt.Fprintf(w, "  ⚑ %s · tm peer decline %s declines it\n", link.ShareSummary(&r), r.ID)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, needsWords(t.Needs))
}

// networkCounts are a link row's unread and queued messages.
func networkCounts(l networkLink) string {
	s := ""
	if l.Unread > 0 {
		s += fmt.Sprintf(" · %d unread", l.Unread)
	}
	if l.Queued > 0 && !strings.Contains(l.Health, "queued") {
		s += fmt.Sprintf(" · %d queued over the rate", l.Queued)
	}
	return s
}

// needsWords is the tree's last line: "NEEDS YOU 3 · 1 message waits
// for your OK · …", or that nothing does.
func needsWords(n networkNeeds) string {
	if n.Total == 0 {
		return "nothing needs you"
	}
	words := []string{fmt.Sprintf("NEEDS YOU %d", n.Total)}
	add := func(k int, one, many string) {
		switch {
		case k == 1:
			words = append(words, "1 "+one)
		case k > 1:
			words = append(words, fmt.Sprintf("%d %s", k, many))
		}
	}
	add(n.Held, "message waits for your OK", "messages wait for your OK")
	add(n.Files, "file waits for you", "files wait for you")
	add(n.LinkRequests, "link request", "link requests")
	add(n.ShareRequests, "share request", "share requests")
	add(n.Paused, "paused link", "paused links")
	return strings.Join(words, " · ")
}
