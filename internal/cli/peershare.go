package cli

import (
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
)

// Share requests (docs/SPEC.md §16.9, Asking for a share): this
// server's user asks a paired server's user to share a project. Sharing
// is a safety setting the console writes on a keypress, so accepting
// one stays in the console (Network); the CLI asks, lists and declines.

// peerAskShare is tm peer ask-share: refused at once (old-tm) when the
// running server's link says that server's tm doesn't take them.
func (e *Env) peerAskShare(who, forSlug, note string) error {
	alias := peer.HomeNames().Alias(who)
	older := false
	if st, _ := peerLinks(); st != nil {
		for _, l := range st.Peers {
			if l.Alias == alias {
				older = slices.Contains(l.Caps, peer.CapMsg) && !slices.Contains(l.Caps, peer.CapShare)
			}
		}
	}
	r, err := link.AskShare(e.Caller, alias, forSlug, note, older)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "%s; its user answers there; tm peer requests shows it\n", link.AskedWords(r))
	return nil
}

// requestEntry is one request in tm peer requests --json: a share
// request (kind share) or a project's link request (kind link).
type requestEntry struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Out  bool   `json:"out"`
	// Peer is the paired server's alias, a link request's other project
	// with it ("mini/api"); Project is the project here.
	Peer    string    `json:"peer"`
	Project string    `json:"project,omitempty"`
	For     string    `json:"for,omitempty"`
	Note    string    `json:"note,omitempty"`
	Allow   string    `json:"allow,omitempty"`
	State   string    `json:"state"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
	Summary string    `json:"summary,omitempty"`
}

// peerRequests is tm peer requests: the share and link requests that
// wait for an answer, both ways, oldest first.
func (e *Env) peerRequests(asJSON bool) error {
	out := []requestEntry{}
	for _, r := range link.ShareRequests() {
		if r.Pending() {
			out = append(out, requestEntry{Kind: "share", ID: r.ID, Out: r.Out, Peer: r.Peer, For: r.For, Note: r.Note,
				State: r.State, Created: r.Created, Expires: r.Created.Add(link.RequestExpiry), Summary: r.Summary})
		}
	}
	list, _ := project.List()
	for _, sum := range list {
		p, err := project.Open(sum.Slug)
		if err != nil {
			continue
		}
		for _, r := range link.Requests(p) {
			if r.Pending() {
				out = append(out, requestEntry{Kind: "link", ID: r.ID, Out: r.Out, Peer: r.Peer, Project: r.Project, Allow: r.Allow,
					State: r.State, Created: r.Created, Expires: r.Created.Add(link.RequestExpiry)})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b requestEntry) int { return a.Created.Compare(b.Created) })
	if asJSON {
		return e.printJSON(map[string]any{"requests": out})
	}
	if len(out) == 0 {
		fmt.Fprintln(e.Stdout, "no requests wait: tm peer ask-share ALIAS asks a paired server to share a project")
		return nil
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	for _, r := range out {
		way := "from"
		if r.Out {
			way = "to"
		}
		what := r.Summary
		if r.Kind == "link" {
			what = fmt.Sprintf("link %s with %s (%s)", r.Project, link.Shown(r.Peer), r.Allow)
		}
		fmt.Fprintf(tw, "%s\t%s %s\t%s\texpires %s\t%s\n", r.ID, way, peer.HomeNames().Show(aliasOf(r.Peer)), r.State,
			r.Expires.Local().Format("2006-01-02 15:04"), what)
	}
	return tw.Flush()
}

// aliasOf is the alias of a request's peer: a share request's is one, a
// link request's "<alias>/<slug>".
func aliasOf(peerKey string) string {
	alias, _, _ := strings.Cut(peerKey, "/")
	return alias
}

// peerDecline is tm peer decline S-…: the share request is declined,
// nothing shares, and its server hears of it.
func (e *Env) peerDecline(id string) error {
	if !link.ValidShareID(id) {
		return usagef("tm peer decline S-…: %q isn't a share request's id (tm peer requests; a link request is tm link decline)", id)
	}
	r, err := link.AnswerShare(e.Caller, id, false, "")
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "declined %s from %s; nothing was shared, %s hears of it\n", r.ID, peer.HomeNames().Show(r.Peer), peer.HomeNames().Show(r.Peer))
	return nil
}
