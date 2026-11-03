package program

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The rolled-up Needs you in words (the programs design §7.1): the
// director's context part and tm program needs. The queue itself is the
// server's (program.needs), since it reads the sessions' menus and
// dialogs.

// Caps on the Needs you part.
const (
	capNeeds     = 20
	capNeedTitle = 60
)

// NeedsSection is the program's Needs you as its context shows it: one
// line per row of res in the queue's order, then the members with
// nothing waiting and those that don't read. res nil: tm's server
// didn't answer, so the menus and dialogs aren't known.
func NeedsSection(res *proto.ProgramNeedsResult) project.Section {
	s := project.Section{Title: "Needs you (rolled up)", Part: project.PartNeeds}
	if res == nil {
		s.Lines = []string{"(not known now: tm's server reads it, and didn't answer; tm program needs)"}
		return s
	}
	for i, n := range res.Needs {
		if i == capNeeds {
			s.Omitted = fmt.Sprintf("%d more (tm program needs)", len(res.Needs)-capNeeds)
			break
		}
		s.Lines = append(s.Lines, NeedLine(n))
	}
	for _, m := range res.Members {
		switch {
		case m.Error != "":
			s.Lines = append(s.Lines, member(m)+": needs unknown ("+textsafe.Clip(m.Error, capNeedTitle)+")")
		case m.Server != "" && m.Count > 0:
			s.Lines = append(s.Lines, fmt.Sprintf("%s: %d waiting on %s (answered there)", member(m), m.Count, m.Server))
		}
	}
	if res.Quiet > 0 {
		word := "projects"
		if res.Quiet == 1 {
			word = "project"
		}
		other := ""
		if len(s.Lines) > 0 {
			other = "other "
		}
		s.Lines = append(s.Lines, fmt.Sprintf("%d %s%s: nothing waiting", res.Quiet, other, word))
	}
	if len(s.Lines) == 0 {
		s.Lines = []string{"(nothing waits on the user)"}
	}
	return s
}

// member names a program member: its slug, or alias/slug on a paired
// server.
func member(m proto.NeedMember) string {
	if m.Server != "" {
		return m.Server + "/" + m.Project
	}
	return m.Project
}

// NeedLine is row n in words: "must acme-api T12 review: OIDC login ·
// acts: accept, send-back". Titles and questions are other parties'
// words, put on one line and cut.
func NeedLine(n proto.ProgramNeed) string {
	var b strings.Builder
	if n.Priority != "" {
		b.WriteString(n.Priority + " ")
	}
	b.WriteString(member(proto.NeedMember{Project: n.Project, Server: n.Server}))
	if ref := needRef(n); ref != "" {
		b.WriteString(" " + ref)
	}
	b.WriteString(" " + needWords(n))
	if what := needWhat(n); what != "" {
		b.WriteString(": " + textsafe.Line(what, capNeedTitle))
	}
	if len(n.Also) > 0 {
		var also []string
		for _, a := range n.Also {
			also = append(also, needWords(proto.ProgramNeed{PeerNeed: a}))
		}
		b.WriteString(" (also " + strings.Join(also, ", ") + ")")
	}
	if len(n.Merged) > 0 {
		b.WriteString(" (covers " + strings.Join(n.Merged, ", ") + ")")
	}
	if len(n.Similar) > 0 {
		b.WriteString(" (similar: " + strings.Join(n.Similar, ", ") + "; tm program ask merge)")
	}
	if r := n.Recommend; r != nil {
		b.WriteString(" · director recommends: " + textsafe.Line(RecommendWords(*r), capNeedTitle) + " — " + textsafe.Line(r.Why, MaxWhy))
	}
	if len(n.Acts) > 0 {
		b.WriteString(" · acts: " + strings.Join(n.Acts, ", "))
	}
	return b.String()
}

// needRef is what row n is on: its task, else its thread, else its
// coordinator question's id.
func needRef(n proto.ProgramNeed) string {
	switch {
	case n.Task != "":
		return n.Task
	case n.Thread != "":
		return n.Thread
	case n.Kind == proto.NeedAsk:
		return strings.TrimPrefix(n.ID, "ask:")
	}
	return ""
}

// needWords say what kind of need row n is, in tm's fixed words.
func needWords(n proto.ProgramNeed) string {
	if n.Escape {
		return "asks to run a command outside the sandbox"
	}
	switch n.Kind {
	case proto.NeedApproval:
		return "permission dialog"
	case proto.NeedQuestion:
		if n.Role == proto.RoleCoordinator {
			return "coordinator asks"
		}
		return "thread asks"
	case proto.NeedReview:
		return "review"
	case proto.NeedUmbrella:
		return "umbrella review"
	case proto.NeedAsk:
		if n.Program {
			return "program question"
		}
		return "coordinator question"
	case proto.NeedCI:
		return "PR red"
	case proto.NeedHost:
		return "PR host failing"
	case proto.NeedBlocked:
		return "blocked"
	case proto.NeedQueue:
		return "prompts held"
	case proto.NeedLinkHeld:
		return "message to " + n.Peer + " held"
	case proto.NeedLinkFile:
		return "file offered by " + n.Peer
	case proto.NeedLinkRequest:
		return "link request from " + n.Peer
	}
	return n.Kind
}

// needWhat is row n's own words: a question's, else its text, else its
// title.
func needWhat(n proto.ProgramNeed) string {
	if n.Escape {
		return n.Title
	}
	if n.Question != nil && len(n.Question.Questions) > 0 {
		return n.Question.Questions[0].Question
	}
	if n.Approval != nil && n.Approval.Question != "" {
		return n.Approval.Question
	}
	if n.Text != "" {
		return n.Text
	}
	return n.Title
}

// WithNeeds is the program's context secs with its Needs you part after
// the Program part, where the director reads it first.
func WithNeeds(secs []project.Section, needs project.Section) []project.Section {
	if len(secs) == 0 {
		return []project.Section{needs}
	}
	out := make([]project.Section, 0, len(secs)+1)
	out = append(out, secs[0], needs)
	return append(out, secs[1:]...)
}

// RecommendWords are recommendation r's answer in words: "Main", "Yes,
// No", "Other: …".
func RecommendWords(r proto.Recommendation) string {
	return project.Answer{Labels: r.Labels, Other: r.Text}.Words()
}
