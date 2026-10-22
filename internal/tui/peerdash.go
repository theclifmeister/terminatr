package tui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The remote dashboard (docs/SPEC.md §4, **Paired servers**): enter, a
// click or the preview on a paired server's project in the sidebar
// shows its dashboard, built from the server's mirror of it
// (peer.list, §16.10: its ProjectWatch and sessions), as the
// coordinator's info panel draws one of this server's (coordpanel.go).
// The view's current project is then "@alias/slug" (peerFold), which no
// slug can be, so every console of the view shows it, the attach view's
// sidebar included.
//
//	" tm dashboard · mini › acme-api                    ● server ok "
//	" ◉ mini · linked 38 ms · synced 2s ago · view-only"
//	" the billing API"                                    its goal
//	" coordinator ● working · 84k / 200k context · opus"
//	" acme-api"                                           the panel
//	" 1 needs you · 2 in inbox · 2 threads"
//	" …"
//
// Nothing here acts on the other server: the keys and the menu items
// that would (open, activate, the project popup, adopt, the mods'
// commands) are left out of the key bar and the ≡ menu, not greyed,
// and say so when pressed; the list keys scroll. The other server is
// reached only through a coordinator link (§16.11).

// peerPlace splits a view's current project that is a paired server's,
// "@alias/slug", into the two; ok is false for this server's projects.
func peerPlace(cur string) (alias, slug string, ok bool) {
	rest, ok := strings.CutPrefix(cur, "@")
	if !ok {
		return "", "", false
	}
	alias, slug, ok = strings.Cut(rest, "/")
	return alias, slug, ok && alias != "" && slug != ""
}

// remote is the paired server's project the dashboard shows, if it shows
// one.
func (m *dash) remote() (alias, slug string, ok bool) { return peerPlace(m.current) }

// treeCurrent is the sidebar tree's current project: the remote one
// shown, else this server's listed one.
func (m *dash) treeCurrent() string {
	if _, _, ok := m.remote(); ok {
		return m.current
	}
	return shownProject(m.data, m.current)
}

// findPeer is the mirror's server alias and its project slug; nil for
// either it doesn't have (any more).
func findPeer(peers []proto.PeerMirror, alias, slug string) (*proto.PeerMirror, *proto.PeerMirrorProject) {
	for i := range peers {
		if peers[i].Alias != alias {
			continue
		}
		pm := &peers[i]
		for j := range pm.Projects {
			if pm.Projects[j].Project == slug {
				return pm, &pm.Projects[j]
			}
		}
		return pm, nil
	}
	return nil, nil
}

// remoteCrumb is the header's breadcrumb for the remote project shown:
// "mini › acme-api".
func remoteCrumb(alias, slug string) string { return alias + " › " + slug }

// remoteLines draws the remote dashboard w cells wide: the link's
// header, the project's goal and coordinator, then the project as the
// coordinator's panel draws it. A stale project (its link down, or its
// last read failed) keeps its last values, dimmed. now is this
// server's clock; the peer's own times are shifted by the mirror's
// offset.
func remoteLines(peers []proto.PeerMirror, alias, slug string, w int, now time.Time) []string {
	pm, p := findPeer(peers, alias, slug)
	var out []string
	add := func(s string) { out = append(out, fit(" "+s, w)+reset) }
	if pm == nil {
		add(th().Head.Render(alias) + th().Faint.Render(" · not paired any more"))
		out = append(out, "")
		add(th().Faint.Render(alias + " isn't paired with this server any more (tm peer list)."))
		return out
	}
	g, gst, words := remoteHead(pm, p, now)
	look := th().Title
	if pm.State != proto.LinkUp {
		look = th().Faint
	}
	add(gst.Render(g) + " " + look.Render(alias) + th().Faint.Render(joinTag(" · "+words, panesTag(pm.Panes))))
	if p == nil {
		out = append(out, "")
		add(th().Faint.Render(alias + " doesn't share " + slug + " with this server any more."))
		return out
	}
	var about []string
	if goal := oneLine(p.Goal); goal != "" {
		about = append(about, goal)
	}
	if p.Paused {
		about = append(about, "paused")
	}
	if len(about) > 0 {
		add(th().Faint.Render(strings.Join(about, " · ")))
	}
	stale := p.Stale || pm.State != proto.LinkUp
	if coord := remoteCoord(p.Sessions); stale {
		add(th().Faint.Render(ansi.Strip(coord)))
	} else {
		add(coord)
	}
	out = append(out, "")
	if p.Watch == nil {
		add(th().Faint.Render("Nothing read from " + alias + " yet."))
		return out
	}
	// The peer's clock: its times (a PR last checked) are its own.
	peerNow := now.Add(time.Duration(pm.Offset) * time.Millisecond)
	pl, _ := watchPanel(p.Watch, w, peerNow, true)
	lines := pl.lines
	if stale {
		lines = slices.Clone(lines)
		for i, l := range lines {
			lines[i] = th().Faint.Render(ansi.Strip(l))
		}
	}
	out = append(out, lines...)
	out = append(out, "")
	for _, l := range wrapLines("To ask its coordinator something, link a project of yours to "+alias+"/"+slug+" (tm project link).", max(w-2, 8)) {
		add(th().Faint.Render(l))
	}
	return out
}

// remoteCoord is the remote project's coordinator line: its state and
// context use, from the mirror's sessions.
func remoteCoord(sessions []proto.PeerSession) string {
	for _, s := range sessions {
		if s.Role != proto.RoleCoordinator {
			continue
		}
		state := oneLine(peerState(s))
		g, st := stateLook(state)
		text := "coordinator " + st.Render(strings.TrimSpace(g+" "+state))
		var bits []string
		if s.Context > 0 && s.ContextWindow > 0 {
			bits = append(bits, kTokens(s.Context)+" / "+kTokens(s.ContextWindow)+" context")
		}
		if s.Model != "" {
			bits = append(bits, oneLine(s.Model))
		}
		if len(bits) > 0 {
			text += th().Faint.Render(" · " + strings.Join(bits, " · "))
		}
		return text
	}
	return "coordinator " + th().Faint.Render("not running")
}

// remoteBody is the remote dashboard as the body's rows, scrolled from
// remoteTop.
func (m *dash) remoteBody() []string {
	alias, slug, _ := m.remote()
	g := m.geom()
	lines := remoteLines(m.data.Peers, alias, slug, g.main().w, sideClock())
	room := g.body().h
	m.remoteTop = min(max(m.remoteTop, 0), max(len(lines)-room, 0))
	body := make([]string, room)
	copy(body, lines[min(m.remoteTop, len(lines)):])
	return body
}

// remoteScroll scrolls the remote dashboard by d lines (the list's up
// and down keys, the wheel); remoteBody keeps it in range.
func (m *dash) remoteScroll(d int) { m.remoteTop = max(m.remoteTop+d, 0) }

// remoteActions are the list actions the remote dashboard keeps: moving
// round this console and this server, never acting on the project
// shown. The rest are absent from its key bar and ≡ menu.
var remoteActions = map[string]bool{
	"list.up": true, "list.down": true, "list.page-up": true, "list.page-down": true,
	"focus.next": true, "focus.prev": true, "place.last": true,
	"project.new": true, "project.next": true, "project.prev": true,
	"sidebar.narrower": true, "sidebar.wider": true, "sidebar.slim": true,
	"settings.open": true, "help.open": true, "console.quit": true,
}

// remoteKey runs a list key on the remote dashboard: the actions it
// keeps, else why nothing happens. ok is false while it doesn't show.
func (m *dash) remoteKey(key string) (tea.Cmd, bool) {
	alias, slug, ok := m.remote()
	if !ok {
		return nil, false
	}
	if _, isCmd := m.commandFor(key); isCmd {
		m.msg = remoteNothing(alias, slug)
		return nil, true
	}
	return m.remoteAction(binds.list.canon(key)), true
}

// remoteAction runs the action of key, a default key, on the remote
// dashboard: one it keeps, else why nothing happens; a key of none does
// nothing. A key typed after the prefix comes here too (prefixCommand),
// so that it never acts on this server's project the list rows are of.
func (m *dash) remoteAction(key string) tea.Cmd {
	alias, slug, _ := m.remote()
	i := slices.IndexFunc(actions, func(a action) bool { return runsAs(a.id, a.keys, key) })
	switch {
	case i < 0:
	case remoteActions[actions[i].id]:
		return actions[i].run(m, runKey(actions[i].id, actions[i].keys))
	case remotePopups[actions[i].id]:
		m.msg = remoteNoPopup(alias, slug)
	default:
		m.msg = remoteNothing(alias, slug)
	}
	return nil
}

// remotePopups are the actions that open the project popup: on the
// remote dashboard they say so, rather than open this server's
// project's. The paired server serves its tasks, settings and messages
// to no one; only a coordinator link reaches it.
var remotePopups = map[string]bool{"project.popup": true, "project.tasks": true, "project.inbox": true}

// projectPopups are the prefix commands that open the project popup.
var projectPopups = map[string]bool{"A": true, "I": true, "T": true}

// remoteNoPopup is why the project popup doesn't open on alias's project
// slug, and where the messages with it are instead.
func remoteNoPopup(alias, slug string) string {
	return slug + " is " + alias + "'s: no project popup here; your messages with it are in the Messages tab of your project linked to it"
}

// remoteNothing is why a key does nothing on alias's project slug.
func remoteNothing(alias, slug string) string {
	return slug + " is " + alias + "'s: nothing here acts on it"
}

// remoteFootKeys is the remote dashboard's key bar: the ≡ menu, the
// scroll and the kept actions the footer shows.
func (m *dash) remoteFootKeys() string {
	out := []string{menuButton + " menu", "↑ ↓ scroll"}
	for _, a := range actions {
		if a.foot == nil || !remoteActions[a.id] {
			continue
		}
		if word := a.foot(m, row{}, false); word != "" {
			out = append(out, footLabel(a)+" "+word)
		}
	}
	return strings.Join(out, " · ")
}

// remoteHere says whether the remote dashboard shows, for the paths
// that skip what it leaves out (dashItems, a click on the list).
func (m *dash) remoteHere() bool {
	_, _, ok := m.remote()
	return ok
}

// shownCrumb is the header's title on the dashboard: the remote
// project's breadcrumb, "all projects" for every project's NEEDS YOU,
// "" for one of this server's.
func (m *dash) shownCrumb() string {
	if alias, slug, ok := m.remote(); ok {
		return remoteCrumb(alias, slug)
	}
	if m.allShown() {
		return "all projects"
	}
	return ""
}
