package cli

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/ticker"
	"github.com/theclifmeister/terminatr/internal/version"
)

const needsUsage = `usage: tm needs [--json]   (what needs you: the dashboard's NEEDS YOU across every project, and the Network's)`

func init() {
	commands["needs"] = runNeeds
	checked["needs"] = true
}

// NeedsSchema is the version of tm needs --json's shape: a front end
// (the Obsidian plugin) built for a newer one asks for a newer tm.
const NeedsSchema = 1

// The kinds of need, each with the actions the console has for it.
const (
	needSession      = "session"       // a blocked session other than a thread's: console
	needQuestion     = "question"      // the coordinator's open question: answer
	needHeld         = "link-held"     // a message waiting for the user's OK: approve, drop
	needFile         = "link-file"     // a file offered over a link: console
	needLinkRequest  = "link-request"  // a paired server's request to link: accept, decline
	needPaused       = "link-paused"   // a link the breaker paused: resume
	needTask         = "task"          // a task in review (accept, send-back) or blocked (delegate)
	needShareRequest = "share-request" // a paired server asks for a share: decline, console
	needRemote       = "remote"        // a paired server's shared project's need: console
)

type (
	// needsList is what tm needs --json prints (docs/SPEC.md §10).
	needsList struct {
		Schema  int    `json:"schema"`
		Version string `json:"version"`
		// Running: a server answered; without one blocked sessions and the
		// paired servers' needs aren't known.
		Running bool   `json:"running"`
		Needs   []need `json:"needs"`
	}
	// need is one thing that waits on the user. Summary is in tm's
	// fixed words, with ids and slugs; Title, Reason, Question and a
	// file's name are other parties' words, data to show framed.
	need struct {
		Kind string `json:"kind"`
		// Project is the project here it is in; Server the paired
		// server's alias for a share request or a remote need (with
		// Project its project).
		Project string `json:"project,omitempty"`
		Server  string `json:"server,omitempty"`
		// ID is what a command takes: T12, Q3, M-18, X-4, R-…, S-…, s-4.
		ID string `json:"id,omitempty"`
		// Peer is the other project of a link need.
		Peer    string `json:"peer,omitempty"`
		Summary string `json:"summary"`
		Status  string `json:"status,omitempty"`
		Title   string `json:"title,omitempty"`
		Thread  string `json:"thread,omitempty"`
		Reason  string `json:"reason,omitempty"`
		// Asked is the kind of the inbox item that already tells the
		// coordinator the user's word on it (delegate, accept, send-back,
		// answer): it waits on the coordinator now.
		Asked    string            `json:"asked,omitempty"`
		Question *project.Question `json:"question,omitempty"`
		File     *link.Transfer    `json:"file,omitempty"`
		// Actions are what the user can do from outside the console, by
		// fixed name; console means only the console can.
		Actions []string `json:"actions"`
	}
)

// runNeeds is tm needs: the human's, read only.
func runNeeds(e *Env, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usagef("%s", needsUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "what needs the user is theirs to read, in their own terminal; the coordinator reads its own (tm context, tm inbox list)"}
	}
	l, err := readNeeds()
	if err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(l)
	}
	e.printNeeds(l)
	return nil
}

// readNeeds lists what needs the user as the dashboard's NEEDS YOU does
// across every project (internal/tui needsRows): by project, its
// blocked sessions, its open questions, what its links wait on, its
// tasks in review or blocked; then the share requests and the paired
// servers' needs; then the user's own blocked sessions outside the
// projects.
func readNeeds() (*needsList, error) {
	l := &needsList{Schema: NeedsSchema, Version: version.Version, Needs: []need{}}
	sessions, running := liveSessions()
	mirror, ok := peerMirror()
	l.Running = running || ok
	paths, err := client.ResolvePaths()
	if err != nil {
		return nil, err
	}
	ps, err := server.ReadProjects(ticker.StatePath(paths.Sessions), nil)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, sp := range ps {
		known[sp.Slug] = true
	}
	var loose []need
	bySlug := map[string][]need{}
	for _, s := range sessions {
		if s.State != "blocked" || s.Role == proto.RoleThread {
			continue
		}
		n := need{Kind: needSession, ID: s.ID, Summary: sessionWords(s) + " is blocked", Reason: textsafe.Line(s.Reason, 200), Actions: []string{"console"}}
		if known[s.Project] {
			n.Project = s.Project
			bySlug[s.Project] = append(bySlug[s.Project], n)
		} else {
			loose = append(loose, n)
		}
	}
	for _, sp := range ps {
		l.Needs = append(l.Needs, bySlug[sp.Slug]...)
		p, err := project.Open(sp.Slug)
		if err != nil {
			continue
		}
		l.Needs = append(l.Needs, questionNeeds(p, sp.Items)...)
		for _, r := range sp.Links {
			l.Needs = append(l.Needs, linkNeeds(p, r)...)
		}
		for _, t := range sp.NeedsYou {
			l.Needs = append(l.Needs, taskNeed(sp.Slug, t.Task(), sp.Items))
		}
	}
	for _, r := range link.ShareRequests() {
		if r.Out || !r.Pending() {
			continue
		}
		l.Needs = append(l.Needs, need{Kind: needShareRequest, Server: r.Peer, ID: r.ID, Summary: link.ShareSummary(&r), Actions: []string{"decline", "console"}})
	}
	if mirror != nil {
		l.Needs = append(l.Needs, remoteNeeds(mirror.Peers)...)
	}
	l.Needs = append(l.Needs, loose...)
	return l, nil
}

// sessionWords name a blocked session: "demo's coordinator", "s-4
// (claude)".
func sessionWords(s proto.SessionInfo) string {
	if s.Role == proto.RoleCoordinator && s.Project != "" {
		return s.Project + "'s coordinator"
	}
	if s.Agent != "" {
		return s.ID + " (" + textsafe.Line(s.Agent, 40) + ")"
	}
	return s.ID
}

// questionNeeds are project p's open questions, each with its options;
// one the user answered already (an answer item waits) waits on the
// coordinator.
func questionNeeds(p *project.Project, items []project.Item) []need {
	qs, err := p.Questions()
	if err != nil {
		return nil
	}
	var out []need
	for i := range qs {
		q := qs[i]
		n := need{Kind: needQuestion, Project: p.Slug, ID: q.ID, Summary: q.ID + " waits for your answer", Question: &q, Actions: []string{"answer"}}
		if q.Task != "" {
			n.Summary = q.ID + " on " + q.Task + " waits for your answer"
		}
		if project.QuestionAnswered(items, q.ID) {
			n.Asked, n.Summary, n.Actions = project.KindAnswer, q.ID+" answered; it waits on the coordinator", []string{}
		}
		out = append(out, n)
	}
	return out
}

// linkNeeds are what link row r of project p waits on: its held
// messages, the files offered, a paired server's request, the link
// paused.
func linkNeeds(p *project.Project, r link.Row) []need {
	if r.Held == 0 && r.FilesWaiting == 0 && r.Asks == nil && !r.Paused {
		return nil
	}
	nl := readLink(p, r, nil, false, nil)
	other := link.Shown(r.Peer)
	var out []need
	for _, id := range nl.Held {
		out = append(out, need{Kind: needHeld, Project: p.Slug, Peer: r.Peer, ID: id, Summary: id + " to " + other + " waits for your OK", Actions: []string{"approve", "drop"}})
	}
	for i := range nl.Files {
		f := nl.Files[i]
		f.Name = textsafe.Line(f.Name, 120)
		out = append(out, need{Kind: needFile, Project: p.Slug, Peer: r.Peer, ID: f.ID, Summary: fmt.Sprintf("a file (%s) from %s waits for you", link.MB(f.Size), other), File: &f, Actions: []string{"console"}})
	}
	if nl.Asks != nil {
		out = append(out, need{Kind: needLinkRequest, Project: p.Slug, Peer: r.Peer, ID: nl.Asks.ID, Summary: other + " asks to link (" + nl.Asks.Allow + " there)", Actions: []string{"accept", "decline"}})
	}
	if nl.Paused {
		out = append(out, need{Kind: needPaused, Project: p.Slug, Peer: r.Peer, ID: r.Peer, Summary: "the link with " + other + " is paused: too many messages without you", Actions: []string{"resume"}})
	}
	return out
}

// taskNeed is task t of project slug in the board's Needs you group: in
// review it takes accept and send-back, blocked delegate, unless an item
// already asks the coordinator about it.
func taskNeed(slug string, t *tasks.Task, items []project.Item) need {
	n := need{Kind: needTask, Project: slug, ID: t.Ref(), Status: string(t.Status), Title: textsafe.Line(t.Title, 200), Thread: t.Thread, Actions: []string{}}
	switch t.Status {
	case tasks.Review:
		n.Summary, n.Actions = t.Ref()+" is in review", []string{"accept", "send-back"}
	default:
		n.Summary, n.Actions = t.Ref()+" is "+string(t.Status), []string{"delegate"}
	}
	if k := project.TaskAsked(items, t.Ref()); k != "" {
		n.Asked, n.Actions = k, []string{}
		n.Summary += "; it waits on the coordinator (" + k + ")"
	}
	return n
}

// remoteNeeds are the needs of the paired servers' shared projects, from
// the mirror's last watch of each: theirs to see, in the console.
func remoteNeeds(peers []proto.PeerMirror) []need {
	var out []need
	for _, pm := range peers {
		for _, mp := range pm.Projects {
			w := mp.Watch
			if w == nil {
				continue
			}
			add := func(id, summary, title string) {
				out = append(out, need{Kind: needRemote, Server: pm.Alias, Project: mp.Project, ID: id, Summary: summary, Title: textsafe.Line(title, 200), Actions: []string{"console"}})
			}
			if w.Questions > 0 {
				add("", project.QuestionsWaiting(w.Questions), "")
			}
			for _, n := range w.NeedsYou {
				ref := cmp.Or(n.Task, n.Thread, n.Session)
				add(ref, strings.TrimSpace(ref+" "+cmp.Or(remoteWhy[n.Why], "needs you")), n.Title)
			}
		}
	}
	return out
}

// remoteWhy say why a paired server's need waits, as its coordinator's
// info panel does.
var remoteWhy = map[string]string{
	proto.WhyQueue:    "prompts held",
	proto.WhyReview:   "in review",
	proto.WhyQuestion: "asks you",
	proto.WhyCI:       "checks failed",
	proto.WhyBlocked:  "blocked",
}

// printNeeds prints the list, a line per need with the command that
// answers it, and the count.
func (e *Env) printNeeds(l *needsList) {
	w := e.Stdout
	for _, n := range l.Needs {
		where := n.Project
		if n.Server != "" {
			where = peer.HomeNames().Show(n.Server)
			if n.Project != "" {
				where += " › " + n.Project
			}
		}
		line := "⚑ " + cmp.Or(where, "outside the projects") + "  " + n.Summary
		if n.Title != "" {
			line += "  " + n.Title
		}
		fmt.Fprintln(w, shown(line))
		if n.Question != nil {
			fmt.Fprintln(w, "    "+shown(project.QuestionLine(*n.Question)))
		}
		if hint := needHint(n); hint != "" {
			fmt.Fprintln(w, "    "+hint)
		}
	}
	switch {
	case len(l.Needs) == 0:
		fmt.Fprintln(w, "nothing needs you")
	default:
		fmt.Fprintf(w, "NEEDS YOU %d\n", len(l.Needs))
	}
	if !l.Running {
		fmt.Fprintln(w, "the server isn't running: blocked sessions and the paired servers' needs aren't known")
	}
}

// needHint is the command that answers n, "" for none outside the
// console.
func needHint(n need) string {
	at := " --project " + n.Project
	switch {
	case n.Asked != "":
		return ""
	case n.Kind == needTask && n.Status == string(tasks.Review):
		return "tm task accept " + n.ID + at + " | tm task send-back " + n.ID + ` --note "…"` + at
	case n.Kind == needTask:
		return "tm task delegate " + n.ID + at
	case n.Kind == needQuestion:
		return "tm ask answer " + n.ID + " --choice N" + at
	case n.Kind == needHeld:
		return "tm link approve|drop " + n.ID + at
	case n.Kind == needLinkRequest:
		return "tm link accept|decline " + n.ID + at
	case n.Kind == needPaused:
		return "tm link resume " + n.Peer + at
	case n.Kind == needShareRequest:
		return "tm peer decline " + n.ID + ", or share a project in Network (N)"
	case n.Kind == needFile, n.Kind == needSession, n.Kind == needRemote:
		return "the console answers it (tm)"
	}
	return ""
}
