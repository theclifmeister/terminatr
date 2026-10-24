package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/plat/open"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// The info panel beside a paired server's pane (docs/SPEC.md §4 and
// §16.12): the pane "<alias>/<id>" has no folder here, so its panel is
// drawn from the server's mirror of that server (peer.list, §16.10), the
// project's ProjectWatch and its trimmed sessions, never from a request
// to the far server. A coordinator's shows its project as the remote
// dashboard does (watchPanel); a thread's its task, steps,
// state and PR. Its head names the server and the link; while the link
// is down, or the last read failed, the rest keeps its last values,
// dimmed, with when they were read. Nothing in it acts on that server:
// no task view, no questions, no mod sections (mods are per server); a
// click on a PR opens it, on a running thread its pane, when that server
// shares its panes with this one.
//
//	" mini"
//	" ◉ connected 38 ms · synced 2s ago"
//	" T7 Billing export"
//	" ● in-progress"
//	" STEPS ───"
//	" …"

// remoteData is what the panel shows of a paired server's pane: the
// server as the mirror has it, and the pane's project and session there.
// project is nil when the server doesn't share it (any more); peer is
// nil when it isn't paired.
type remoteData struct {
	// alias keys the server; shown is what the panel calls it, its
	// display name (tm peer name) when it has one.
	alias, shown string
	peer         *proto.PeerMirror
	project      *proto.PeerMirrorProject
	// sess is the pane's session as the mirror lists it; found says it
	// does.
	sess  proto.PeerSession
	found bool
}

// loadRemoteInfo is the panel's data for in, a paired server's pane,
// from peers, the mirror; nil for a pane that is neither a coordinator
// nor a thread.
func loadRemoteInfo(peers []proto.PeerMirror, in proto.SessionInfo) *infoData {
	alias, sid, ok := strings.Cut(in.ID, "/")
	if !ok {
		return nil
	}
	alias = cmp.Or(in.Server, alias)
	r := &remoteData{alias: alias, shown: peerShown(peers, alias)}
	for i := range peers {
		if peers[i].Alias != alias {
			continue
		}
		pm := peers[i]
		pm.Projects = nil
		r.peer = &pm
		for _, pr := range peers[i].Projects {
			for _, s := range pr.Sessions {
				if s.ID == sid {
					r.sess, r.found = s, true
				}
			}
			if pr.Project == in.Project || in.Project == "" && r.found && r.project == nil {
				p := pr
				r.project = &p
			}
		}
	}
	if !r.found && r.project != nil {
		// The far server restarted it as another session: the
		// coordinator, or the thread, by its project.
		for _, s := range r.project.Sessions {
			if s.Role == in.Role && (s.Role == proto.RoleCoordinator || s.Thread == in.Thread && in.Thread != "") {
				r.sess, r.found = s, true
				break
			}
		}
	}
	role, slug := in.Role, in.Project
	if r.found {
		role, slug = r.sess.Role, r.sess.Project
	}
	if role != proto.RoleCoordinator && role != proto.RoleThread {
		return nil
	}
	return &infoData{slug: peerFold(alias, slug), session: in, remote: r}
}

// stale says the panel shows the project's last values: its link is
// down, or the last read failed.
func (r *remoteData) stale() bool {
	return r.peer == nil || r.peer.State != proto.LinkUp || r.project == nil || r.project.Stale
}

// remoteHead is a paired server's link as a remote project's head says
// it, for project p (nil: not shared): its glyph and style, and its
// words, "connected 38 ms · synced 2s ago", "offline 4m · relay · last
// seen 14:02".
func remoteHead(pm *proto.PeerMirror, p *proto.PeerMirrorProject, now time.Time) (g string, gst lipgloss.Style, words string) {
	link := &peerLink{state: pm.State, reason: pm.Reason, since: pm.Since, rtt: pm.RTT, watch: pm.Watch}
	g, gst = linkLook(link)
	words = linkWords(link, now)
	if pm.State == proto.LinkUp && pm.RTT > 0 {
		words = "connected " + words
	}
	switch {
	case p == nil:
	case p.Stale || pm.State != proto.LinkUp:
		if !p.Synced.IsZero() {
			words += " · last seen " + p.Synced.Local().Format("15:04")
		}
	case p.Synced.IsZero():
		words += " · not read yet"
	default:
		words += " · synced " + age(max(now.Sub(p.Synced), 0)) + " ago"
	}
	return g, gst, words
}

// remotePanel is a paired server's pane's panel w cells wide, with what
// a click on each line does: the head (the server, its link), then the
// coordinator's project or the thread, dimmed while stale, then the
// note on what acts on the server. now is this server's clock; the peer's own times are
// shifted by the mirror's offset.
func remotePanel(d *infoData, w int, now time.Time) (*panel, []infoHit) {
	r := d.remote
	pl := &panel{w: w}
	var hits []infoHit
	pad := func() {
		hits = padHits(hits, len(pl.lines), infoHit{})
	}
	// The head: the server's name on a line of its own, so a long one
	// is cut, never the link's words, which wrap under it.
	if r.peer == nil {
		pl.add(th().Head.Render(oneLine(r.shown)))
		hang(pl, "", th().Faint.Render("not paired any more"))
		pl.gap()
		pl.wrap(th().Faint.Render(r.shown + " isn't paired with this server any more (tm peer list)."))
		pad()
		return pl, hits
	}
	g, gst, words := remoteHead(r.peer, r.project, now)
	look := th().Title
	if r.peer.State != proto.LinkUp {
		look = th().Faint
	}
	pl.add(look.Render(oneLine(r.shown)))
	hang(pl, gst.Render(g)+" ", th().Faint.Render(words))
	pl.mustFrom(0)
	pad()
	if r.project == nil {
		pl.gap()
		pl.wrap(th().Faint.Render(r.shown + " doesn't share this project with this server any more."))
		pad()
		return pl, hits
	}
	pl.gap()
	pad()
	if r.project.Watch == nil {
		pl.wrap(th().Faint.Render("Nothing read from " + r.shown + " yet."))
		pad()
		return pl, hits
	}
	peerNow := now.Add(time.Duration(r.peer.Offset) * time.Millisecond)
	var body *panel
	var bodyHits []infoHit
	if r.sess.Role == proto.RoleCoordinator || !r.found && d.session.Role == proto.RoleCoordinator {
		body, bodyHits = watchPanel(r.project.Watch, w, peerNow, true)
		body.lines = append(slices.Clone(body.lines[:1]), append([]string{fit(" "+remoteCoordLine(r), w) + reset}, body.lines[1:]...)...)
		bodyHits = append(slices.Clone(bodyHits[:1]), append([]infoHit{{}}, bodyHits[1:]...)...)
	} else {
		body, bodyHits = remoteThread(r, d.session, w, peerNow)
	}
	stale := r.stale()
	for i, l := range body.lines {
		if stale {
			l = th().Faint.Render(ansi.Strip(l))
		}
		pl.lines = append(pl.lines, l)
		h := infoHit{}
		if i < len(bodyHits) {
			h = r.hit(bodyHits[i])
		}
		hits = append(hits, h)
	}
	return pl, hits
}

// remoteCoordLine is the coordinator's line under its project's name:
// its state and model; its context use is the panel's next lines.
func remoteCoordLine(r *remoteData) string {
	if !r.found {
		return "coordinator " + th().Faint.Render("not running")
	}
	state := peerState(r.sess)
	g, st := stateLook(state)
	text := "coordinator " + st.Render(strings.TrimSpace(g+" "+state))
	if r.sess.Model != "" {
		text += th().Faint.Render(" · " + oneLine(r.sess.Model))
	}
	return text
}

// hit is what a click on a remote panel's line does, of what h would do
// in this server's: a PR opens; a running thread shows its pane when
// the server shares its panes with this one; a task, the questions and
// the rest would act here, on another server's names, and do nothing.
func (r *remoteData) hit(h infoHit) infoHit {
	switch h.kind {
	case hitPR:
		return h
	case hitSession:
		row := treeRow{peer: r.alias, session: h.session, link: &peerLink{state: r.peer.State, pane: r.peer.Pane, panes: r.peer.Panes, version: r.peer.Version, name: r.peer.Name}}
		if paneWhyNot(row) == "" {
			return infoHit{kind: hitSession, session: r.alias + "/" + h.session}
		}
	}
	return infoHit{}
}

// remoteThread is a paired server's thread's panel, from the mirror:
// its task with its steps' progress and the one under way, its state,
// progress and model, what it needs from the user, its context use and
// its PR with the checks. The step texts and its report stay there.
func remoteThread(r *remoteData, in proto.SessionInfo, w int, now time.Time) (*panel, []infoHit) {
	pl := &panel{w: w}
	var hits []infoHit
	hit := func(from int, h infoHit) {
		hits = padHits(hits, len(pl.lines), infoHit{})
		for i := from; i < len(hits); i++ {
			hits[i] = h
		}
	}
	s := r.sess
	id := cmp.Or(s.Thread, in.Thread)
	var wt *proto.WatchThread
	for i := range r.project.Watch.Threads {
		if r.project.Watch.Threads[i].ID == id {
			wt = &r.project.Watch.Threads[i]
		}
	}
	pl.block("core")
	switch {
	case wt != nil && wt.Task != nil:
		t := wt.Task
		pl.wrap(th().Title.Render(oneLine(t.ID + " " + t.Title)))
		g, st := stateLook(t.Status)
		pl.add(st.Render(strings.TrimSpace(g + " " + t.Status)))
		if t.StepsTotal > 0 {
			pl.section("STEPS", "")
			pl.add(progressLine(thread.Progress{Percent: pctOf(t.StepsDone, t.StepsTotal), Done: t.StepsDone, Total: t.StepsTotal}))
			if t.Current != "" {
				hang(pl, todoGlyph("in_progress")+" ", th().Accent.Render(oneLine(t.Current)))
			}
		}
	case wt != nil:
		pl.wrap(th().Title.Render(oneLine(wt.Title)))
		pl.add(th().Faint.Render("no task"))
	}
	pl.section("THREAD", "")
	state := "stopped"
	switch {
	case wt != nil:
		state = watchThreadState(*wt)
	case r.found:
		state = peerState(s)
	}
	if r.found && s.State == "blocked" && s.Reason != "" && s.Reason != "question" {
		state += " " + s.Reason
	}
	g, st := stateLook(state)
	if state == "asks you" {
		g, st = stateLook("blocked")
	}
	pl.field("thread", id+" "+st.Render(strings.TrimSpace(g+" "+oneLine(state))))
	if r.found {
		pl.field("model", oneLine(s.Model))
		if s.TodosTotal > 0 {
			pl.field("todos", progressLine(thread.Progress{Percent: pctOf(s.TodosDone, s.TodosTotal), Done: s.TodosDone, Total: s.TodosTotal}))
		}
		if s.Current != "" && (wt == nil || wt.Task == nil || wt.Task.Current != s.Current) {
			pl.field("now", "▸ "+oneLine(s.Current))
		}
		if s.Context > 0 && s.ContextWindow > 0 {
			pl.field("context", th().Faint.Render(fmt.Sprintf("%s / %s · %d%%", kTokens(s.Context), kTokens(s.ContextWindow), s.Context*100/s.ContextWindow)))
		}
	}
	if wt != nil && wt.NeedsYou != "" {
		from := len(pl.lines)
		hang(pl, th().Faint.Render(fmt.Sprintf("%-9s", "needs you"))+" ", th().Warn.Render(oneLine(wt.NeedsYou)))
		pl.mustFrom(from)
	}
	if r.found && s.Asking {
		from := len(pl.lines)
		hang(pl, th().Faint.Render(fmt.Sprintf("%-9s", "question"))+" ", th().Warn.Render("open in its pane"))
		pl.mustFrom(from)
	}
	// The PR and its checks, as the ticker there last saw them.
	if wt != nil && wt.PR != "" {
		look := th().Accent
		switch {
		case wt.PRBad:
			look = th().Bad
		case strings.Contains(wt.PR, "merged"):
			look = th().Good
		}
		from := len(pl.lines)
		pr := look.Render(oneLine(wt.PR))
		if c := proto.CheckedWords(wt.PRChecked, now); c != "" {
			pr += th().Faint.Render(" · " + c)
		}
		hang(pl, th().Faint.Render(fmt.Sprintf("%-9s", "PR"))+" ", pr)
		switch wt.PRChecks {
		case "pass":
			pl.field("checks", th().Good.Render("passing"))
		case "fail":
			pl.field("checks", th().Bad.Render("failing"))
		case "pending":
			pl.field("checks", th().Warn.Render("running"))
		}
		if open.Web(wt.PRURL) {
			hit(from, infoHit{kind: hitPR, url: wt.PRURL})
		}
		if wt.PRBad {
			pl.mustFrom(from)
		}
	}
	if wt != nil && wt.Reports > 0 {
		pl.field("reports", fmt.Sprint(wt.Reports))
	}
	hits = padHits(hits, len(pl.lines), infoHit{})
	return pl, hits
}
