package tui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// Server groups (docs/SPEC.md §4, **Paired servers**): with paired
// servers (§16), the sidebar's tree gets a level above the projects, a
// header row per server, this one first, then each paired server's
// shared projects from the server's mirror of them (peer.list, §16.10).
// With none it is drawn as it always was: no header at all.
//
//	" ◉ studio · this server            "
//	" ■ terminatr          3 ●        ⚑ "   this server's tree, as before
//	" ◉ mini · 38 ms                    "   linked, the link's last ping
//	" ■ acme-api           2          ⚑ "   its shared projects, view-only
//	" ├─ coordinator     22% ●          "
//	" ○ mini · offline 4m · relay       "   offline: how long, and why
//	" ■ acme-api           2 ·        ⚑ "   last values, dimmed, no state
//
// The remote rows are kinds of their own (treePeerProject, …), drawn as
// a project, coordinator and thread are, so no key, click or preview
// meant for this server's projects reaches them. Enter on a remote
// coordinator or thread opens its pane (§16.12) when the paired server
// shares its panes with this one: the session "<alias>/<id>". A paired server's
// group folds as a project does, and so does each of its projects;
// both are kept in the view's tree with the projects (view.Tree.Folded)
// as "@alias" and "@alias/slug", which no slug can be.

// peerLink is a server header's link, and a remote row's: its state as
// the mirror last had it.
type peerLink struct {
	// local: this server's header; host is its name.
	local bool
	host  string
	// state, reason and since are the link's (proto.LinkStatus); rtt
	// its last ping in milliseconds.
	state, reason string
	since         time.Time
	rtt           float64
	// watch: the peer shares projects (its hello lists watch).
	watch bool
	// synced is when this server last read the row's project, or the
	// group's latest; zero for never.
	synced time.Time
	// pane: the peer has remote panes; panes is its switch for this
	// server (proto.PanesOff …), version its tm's release.
	pane           bool
	panes, version string
}

// sideClock is the clock the server headers' ages are drawn by; tests
// pin it.
var sideClock = time.Now

// thisHost is this machine's name for its server header: the host name
// up to its first dot; "this server" when there is none.
var thisHost = sync.OnceValue(func() string {
	h, _ := os.Hostname()
	h, _, _ = strings.Cut(h, ".")
	if h == "" {
		return "this server"
	}
	return h
})

// peerFold is the view.Tree.Folded entry of a paired server's group
// (slug "") or of one of its projects.
func peerFold(alias, slug string) string {
	if slug == "" {
		return "@" + alias
	}
	return "@" + alias + "/" + slug
}

// peerKey names a server header or a remote row as View.SideSel does:
// "g:" for this server's header, "g:<alias>" for a paired one's,
// "r:<alias>/<project>" for its project, "rc:…" for the project's
// coordinator and "rt:<alias>/<project>/<thread>" for a thread.
func peerKey(r treeRow) string {
	switch r.kind {
	case treePeerProject:
		return "r:" + r.peer + "/" + r.slug
	case treePeerCoord:
		return "rc:" + r.peer + "/" + r.slug
	case treePeerThread:
		return "rt:" + r.peer + "/" + r.slug + "/" + r.thread
	}
	return "g:" + r.peer
}

// isPeer says r is a paired server's: its header or one of its rows.
func (r treeRow) isPeer() bool { return r.peer != "" }

// withPeers puts the server groups round tree, this server's rows: none
// without peers; else this server's header above them and each paired
// server's group after them, by alias.
func withPeers(tree []treeRow, in treeIn) []treeRow {
	if len(in.peers) == 0 {
		return tree
	}
	host := in.host
	if host == "" {
		host = thisHost()
	}
	out := make([]treeRow, 0, len(tree)+1+4*len(in.peers))
	out = append(out, treeRow{kind: treeServer, pct: -1, link: &peerLink{local: true, host: host, state: proto.LinkUp}})
	out = append(out, tree...)
	for _, pm := range in.peers {
		out = append(out, peerRows(pm, in.tree)...)
	}
	// A remote pane in the view is you are on its row.
	if _, _, remote := strings.Cut(in.focus, "/"); remote {
		for i, r := range out {
			if (r.kind == treePeerCoord || r.kind == treePeerThread) && r.session != "" && in.focus == r.peer+"/"+r.session {
				for j := range out {
					out[j].here = false
				}
				out[i].here = true
				break
			}
		}
	}
	return out
}

// peerTarget is where enter on a remote row goes: a coordinator's or a
// thread's pane, when its server shares panes with this one; else why
// not.
func peerTarget(r treeRow) (t Target, ok bool, why string) {
	if r.kind != treePeerCoord && r.kind != treePeerThread {
		return t, false, peerWhy(r)
	}
	if why := paneWhyNot(r); why != "" {
		return t, false, why
	}
	return Target{Session: r.peer + "/" + r.session}, true, ""
}

// paneWhyNot is why remote row r's pane doesn't open; "" when it does.
func paneWhyNot(r treeRow) string {
	l := r.link
	switch {
	case l.state != proto.LinkUp:
		return r.peer + " is offline: its panes open once it is back"
	case !l.pane:
		return r.peer + " runs tm " + l.version + ", which has no remote panes; update it to open them"
	case l.panes != proto.PanesView && l.panes != proto.PanesType:
		return r.peer + " doesn't share panes with this server: on " + r.peer + ", tm peer panes <this server> view, or type"
	case r.session == "":
		return "nothing runs for it on " + r.peer
	}
	return ""
}

// paneWords are the footer's words on a remote coordinator's or thread's
// pane: whether enter opens it, and how.
func paneWords(r treeRow) string {
	switch {
	case paneWhyNot(r) != "" && r.link.state == proto.LinkUp && r.link.pane && r.session != "":
		return "panes off"
	case paneWhyNot(r) != "":
		return "view-only"
	case r.link.panes == proto.PanesView:
		return "its pane: view only"
	}
	return "its pane opens here"
}

// peerRows are a paired server's group: its header, then unless it is
// folded each shared project's row, and under a project that is up to
// date and not folded its coordinator and threads. A stale project
// (its link down, or its last read failed) shows its row alone, its
// last values dimmed.
func peerRows(pm proto.PeerMirror, t view.Tree) []treeRow {
	link := peerLink{state: pm.State, reason: pm.Reason, since: pm.Since, rtt: pm.RTT, watch: pm.Watch,
		pane: pm.Pane, panes: pm.Panes, version: pm.Version}
	for _, p := range pm.Projects {
		if p.Synced.After(link.synced) {
			link.synced = p.Synced
		}
	}
	head := treeRow{kind: treeServer, peer: pm.Alias, pct: -1, link: &link, threads: len(pm.Projects),
		fold: t.IsFolded(peerFold(pm.Alias, ""))}
	var rows []treeRow
	for _, p := range pm.Projects {
		pl := link
		pl.synced = p.Synced
		rows = append(rows, peerProjectRows(pm.Alias, p, &pl, t)...)
	}
	for _, r := range rows {
		head.hint = head.hint || r.kind == treePeerProject && r.hint
	}
	if head.fold {
		return []treeRow{head}
	}
	return append([]treeRow{head}, rows...)
}

// peerProjectRows are a paired server's shared project's rows: its own,
// and unless it is stale or folded its coordinator's and its threads',
// as projectRows lays out a project of this server.
func peerProjectRows(alias string, p proto.PeerMirrorProject, link *peerLink, t view.Tree) []treeRow {
	pr := treeRow{kind: treePeerProject, peer: alias, slug: p.Project, pct: -1, link: link, threads: p.Threads,
		hint: p.NeedsYou > 0, paused: p.Paused, stale: p.Stale}
	ctx, asks := -1, 0
	byID := map[string]proto.PeerSession{}
	for _, s := range p.Sessions {
		byID[s.ID] = s
		if s.Role == proto.RoleCoordinator && pr.session == "" {
			pr.session, pr.state, pr.remote = s.ID, peerState(s), s.RemoteControl
			if s.Context > 0 && s.ContextWindow > 0 {
				ctx = int(s.Context * 100 / s.ContextWindow)
			}
		}
	}
	if p.Watch != nil {
		asks = p.Watch.Questions
		pr.hint = pr.hint || asks > 0
	}
	var kids []treeRow
	if p.Watch != nil {
		for _, w := range p.Watch.Threads {
			tr := treeRow{kind: treePeerThread, peer: alias, slug: p.Project, thread: w.ID, title: oneLine(w.Title),
				pct: -1, link: link, stale: p.Stale, ci: w.PRChecks, session: w.Session}
			if w.Task != nil {
				tr.name, tr.task = w.Task.ID, w.Task.ID
				if w.Task.StepsTotal > 0 {
					tr.pct = w.Task.StepsDone * 100 / w.Task.StepsTotal
				}
			}
			tr.name = threadName(tr.task, w.ID)
			tr.state = w.State
			if s, ok := byID[w.Session]; ok && w.Session != "" {
				tr.state = peerState(s)
				if tr.pct < 0 && s.TodosTotal > 0 {
					tr.pct = s.TodosDone * 100 / s.TodosTotal
				}
			}
			if w.Done && tr.state != "blocked" && tr.state != "working" {
				tr.state = "done"
			}
			tr.hint = tr.state == "blocked" || tr.state == "review" || w.NeedsYou != ""
			pr.hint = pr.hint || tr.hint
			kids = append(kids, tr)
		}
	}
	if pr.stale || t.IsFolded(peerFold(alias, p.Project)) {
		pr.fold = true
		return []treeRow{pr}
	}
	coord := treeRow{kind: treePeerCoord, peer: alias, slug: p.Project, session: pr.session, state: pr.state, remote: pr.remote,
		pct: -1, last: true, link: link, hint: asks > 0, asks: asks, ctx: ctx}
	if len(kids) > 0 {
		kids[len(kids)-1].last = true
	}
	return append([]treeRow{pr, coord}, kids...)
}

// peerState is a remote session's state word, as stateWord has a local
// one's.
func peerState(s proto.PeerSession) string {
	switch {
	case s.State != "":
		return s.State
	case s.Agent != "":
		return "starting"
	}
	return "running"
}

// drawn is remote row r as the local kind it is drawn as: a project, a
// coordinator or a thread. Only drawing uses it.
func (r treeRow) drawn() treeRow {
	switch r.kind {
	case treePeerProject:
		r.kind = treeProject
	case treePeerCoord:
		r.kind = treeCoordinator
	case treePeerThread:
		r.kind = treeThread
	}
	return r
}

// linkLook is a server header's glyph and its style: linked, connecting,
// offline, or refused (the peer's key isn't the paired one, or its tm is
// too old to link).
func linkLook(l *peerLink) (string, lipgloss.Style) {
	i := ic()
	switch {
	case l.local || l.state == proto.LinkUp:
		return i.linked, th().Good
	case l.state == proto.LinkConnecting:
		return i.starting, th().Warn
	case l.reason == proto.LinkReasonRefused || l.reason == proto.LinkReasonVersion:
		return i.inactive, th().Bad
	}
	return i.idle, th().Faint
}

// linkWords are a server header's words after its name: "this server";
// the link's last ping ("38 ms"); "connecting"; "offline 4m · relay",
// how long and why.
func linkWords(l *peerLink, now time.Time) string {
	switch {
	case l.local:
		return "this server"
	case l.state == proto.LinkUp && l.rtt > 0:
		return rttWords(l.rtt)
	case l.state == proto.LinkUp:
		return "linked"
	case l.state == proto.LinkConnecting:
		return "connecting"
	}
	w := "offline"
	if !l.since.IsZero() {
		w += " " + age(max(now.Sub(l.since), 0))
	}
	if why := linkReasonWords[l.reason]; why != "" {
		w += " · " + why
	}
	return w
}

// linkReasonWords say why a link is offline (proto.LinkReason*).
var linkReasonWords = map[string]string{
	proto.LinkReasonRelay:   "relay",
	proto.LinkReasonPeer:    "peer",
	proto.LinkReasonRefused: "key refused",
	proto.LinkReasonVersion: "old tm",
}

// rttWords is a round trip in milliseconds: "38 ms", "<1 ms".
func rttWords(ms float64) string {
	if ms < 1 {
		return "<1 ms"
	}
	return fmt.Sprintf("%d ms", int(math.Round(ms)))
}

// serverCells is a server header's row cw cells wide: " ◉ mini · 38 ms",
// its glyph in the link's colour, its name bold (faint while the link
// is down), the words faint; folded, how many projects it hides. In the
// slim strip, with no room for names, its glyph and a rule in the
// link's colour mark where the group starts.
func serverCells(r treeRow, cw int, slim bool, sel lipgloss.Style) string {
	g, gst := linkLook(r.link)
	if slim {
		line := g + " " + strings.Repeat("─", max(cw-4, 0))
		if r.here {
			return fit(" "+sel.Render(line), cw)
		}
		return fit(" "+gst.Render(line), cw)
	}
	name := r.peer
	if r.link.local {
		name = r.link.host
	}
	words := " · " + linkWords(r.link, sideClock())
	if r.fold {
		words += " · " + counted(r.threads, "project")
	}
	look := th().Title
	if !r.link.local && r.link.state != proto.LinkUp {
		look = th().Faint
	}
	// The margin, the glyph and its blank, and the needs-you column.
	room := max(cw-1-ansi.StringWidth(g)-1-2, 0)
	name = fitLabel(name, min(ansi.StringWidth(name), room))
	if ww := room - ansi.StringWidth(name); ansi.StringWidth(words) > ww {
		words = fitLabel(words, ww)
	}
	hint := cellRun{"  ", th().Plain}
	if r.hint && r.fold {
		hint = cellRun{ic().hint + " ", th().Warn}
	}
	rest := []cellRun{{g + " ", gst}, {name, look}, {words, th().Faint}}
	pad := max(cw-1-runsWidth(rest)-runsWidth([]cellRun{hint}), 0)
	rest = append(rest, cellRun{strings.Repeat(" ", pad), th().Plain}, hint)
	if r.here {
		return fit(" "+sel.Render(runsPlain(rest)), cw)
	}
	return fit(" "+runsRender(rest), cw)
}

// peerDetails are the footer's two lines about a server header or a
// remote row.
func peerDetails(r treeRow) (string, string) {
	running := func(state string) string {
		if state == "" {
			return "not running"
		}
		return state
	}
	l := r.link
	seen := func() string {
		if l.synced.IsZero() {
			return "not read yet"
		}
		return "last seen " + l.synced.Local().Format("15:04")
	}
	where := r.peer + " / " + r.slug
	switch r.kind {
	case treeServer:
		if l.local {
			return l.host + " · this server", "its projects are below, each paired server's after them"
		}
		l1 := r.peer + " · " + linkWords(l, sideClock())
		if l.state == proto.LinkUp {
			l1 = r.peer + " · linked · " + linkWords(l, sideClock())
		}
		switch {
		case l.state != proto.LinkUp && r.threads > 0:
			return l1, seen() + " · its projects keep their last values"
		case l.state == proto.LinkUp && !l.watch:
			return l1, "its tm shares no projects with this one"
		case r.threads == 0:
			return l1, "shares no projects with this server"
		}
		return l1, counted(r.threads, "shared project") + " · view-only"
	case treePeerProject:
		bits := []string{where, "view-only"}
		if r.paused {
			bits = append(bits, "paused")
		}
		if r.stale {
			return strings.Join(bits, " · "), seen() + " · " + counted(r.threads, "thread")
		}
		return strings.Join(bits, " · "), fmt.Sprintf("%s · coordinator %s", counted(r.threads, "thread"), running(r.state))
	case treePeerCoord:
		bits := []string{paneWords(r)}
		if r.ctx >= 0 {
			bits = append(bits, fmt.Sprintf("context %d%%", r.ctx))
		}
		if r.asks > 0 {
			bits = append(bits, counted(r.asks, "question")+" waiting")
		}
		return where + " coordinator · " + running(r.state), strings.Join(bits, " · ")
	}
	l1 := r.peer + " / " + r.thread
	switch {
	case r.name != r.thread:
		l1 = r.peer + " / " + strings.TrimSpace(r.name+" "+r.title) + " (" + r.thread + ")"
	case r.title != "":
		l1 = r.peer + " / " + r.thread + " " + r.title
	}
	bits := []string{running(r.state)}
	if r.pct >= 0 {
		bits = append(bits, fmt.Sprintf("%d%%", r.pct))
	}
	return l1, strings.Join(append(bits, paneWords(r)), " · ")
}

// peerWhy is why a remote row opens nothing: the other server is only
// read here.
func peerWhy(r treeRow) string {
	if r.kind == treeServer {
		if r.link.local {
			return "this server's projects are below"
		}
		return "enter on a paired server folds or unfolds it"
	}
	return r.slug + " is " + r.peer + "'s: view-only here"
}

// peerFoldStep is what → ← and enter do to the folds on a server header
// or a remote project (foldStep): a paired server's header folds and
// unfolds with all three, a remote project with ← → as a project does
// (← on it folded goes up to its server); a stale one has nothing to
// unfold. ok is false where the key does something else.
func peerFoldStep(rows []treeRow, i, op int, slim bool) (sideStep, bool) {
	r := rows[i]
	switch {
	case r.kind == treeServer && r.link.local:
		if op == sideEnter {
			return sideStep{msg: peerWhy(r)}, true
		}
	case r.kind == treeServer && (op == sideIn && r.fold || op == sideOut && !r.fold || op == sideEnter):
		return sideStep{refold: foldPeer(r, !r.fold)}, true
	case r.kind != treePeerProject || slim:
	case op == sideIn && r.fold && !r.stale, op == sideOut && !r.fold:
		return sideStep{refold: foldPeer(r, !r.fold)}, true
	case op == sideOut:
		for j := i - 1; j >= 0; j-- {
			if rows[j].kind == treeServer {
				return sideStep{sel: rows[j].key()}, true
			}
		}
	}
	return sideStep{}, false
}

// foldPeer folds (fold) or unfolds a paired server's group or one of
// its projects in the view's tree.
func foldPeer(r treeRow, fold bool) func(*view.Tree) {
	id := peerFold(r.peer, "")
	if r.kind == treePeerProject {
		id = peerFold(r.peer, r.slug)
	}
	return func(t *view.Tree) { t.Fold(id, fold) }
}

// peerFolds are the folds z adds for the server groups in all: each
// paired server's projects; Z, which unfolds everything, drops them.
func peerFolds(all []treeRow) []string {
	var out []string
	for _, r := range all {
		if r.kind == treePeerProject {
			out = append(out, peerFold(r.peer, r.slug))
		}
	}
	return out
}

// peerKeyBar is the footer's keys on a server header or a remote row.
func peerKeyBar(r *treeRow) []string {
	enter, left := barKey("side.enter", "⏎"), barKey("side.fold", "←")
	switch r.kind {
	case treeServer:
		switch {
		case r.link.local:
			return nil
		case r.fold:
			return []string{enter + " unfold"}
		}
		return []string{enter + " fold"}
	case treePeerProject:
		switch {
		case r.stale:
			return []string{left + " server"}
		case r.fold:
			return []string{barKey("side.unfold", "→") + " unfold"}
		}
		return []string{left + " fold"}
	}
	if paneWhyNot(*r) == "" {
		return []string{enter + " open", left + " project"}
	}
	return []string{left + " project"}
}
