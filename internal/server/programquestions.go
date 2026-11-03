package server

// A director's /answer (the programs design §7.2, §7.3): the program's
// open questions open as tm's own question on the director's session,
// which only the user answers (Question.HumanOnly), in the consoles'
// question dialog. Once each has its answer, the server records it as
// the user's (program.Answer): every member question it covers gets
// the answer, mapped to its options, and the director a program-answer
// item. The director opens the dialog and never fills it.

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// ProgramQuestionsTool is the Tool of the question a director's /answer
// opens: the program's questions.
const ProgramQuestionsTool = "tm program"

// openProgramQuestions opens program slug's open questions, up to the
// dialog's MaxDialogQuestions, on director session id for the user,
// and records their answers when all have one. It says how many it
// opened.
func (s *Server) openProgramQuestions(slug, id string) (int, *proto.Error) {
	cfg, _ := config.Load()
	if _, perr := directorReady(cfg, slug); perr != nil {
		return 0, perr
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return 0, openError(err)
	}
	qs, err := g.Questions()
	if err != nil {
		return 0, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	if len(qs) == 0 {
		return 0, proto.Errorf(proto.ErrRefused, "program %s has no open questions (the director merges members' alike questions with tm program ask merge)", slug)
	}
	sess, perr := s.session(id)
	if perr != nil {
		return 0, perr
	}
	qs = qs[:min(len(qs), project.MaxDialogQuestions)]
	recs, _ := g.Recommendations()
	q := proto.Question{Tool: ProgramQuestionsTool, HumanOnly: true, Since: time.Now().UTC()}
	for i, pq := range qs {
		// The director's recommended options first, marked as the
		// dialog marks an agent's own (T982).
		stored, has := recs[slug+"/"+pq.ID]
		r, _ := program.Current(stored, has, pq)
		qs[i] = recommendedFirst(pq, r.Labels)
		pq = qs[i]
		item := questionItem(pq)
		for j := range item.Options {
			if slices.Contains(r.Labels, item.Options[j].Label) {
				item.Options[j].Label += RecommendedMark
			}
		}
		item.Header = pq.ID
		if pq.Header != "" {
			item.Header = textClip(pq.ID+" "+pq.Header, project.MaxQuestionHeader)
		}
		q.Questions = append(q.Questions, item)
	}
	o := s.asks.put(id, q)
	s.watch.wake()
	s.modEv.wake()
	go func() {
		defer s.asks.drop(id, o)
		defer s.watch.wake()
		defer s.modEv.wake()
		select {
		case ans := <-o.answered:
			s.recordProgramAnswers(slug, qs, ans)
		case <-o.replaced:
		case <-sess.Done():
		}
	}()
	return len(qs), nil
}

// recordProgramAnswers records the user's answers in the dialog (by
// question text) to program slug's questions qs, as tm ask answer
// <program>/Q2 does. A question answered or closed meanwhile is left.
func (s *Server) recordProgramAnswers(slug string, qs []project.Question, ans map[string]string) {
	cfg, _ := config.Load()
	g, err := program.Open(slug, cfg)
	if err != nil {
		s.log.Printf("program %s: answers: %v", slug, err)
		return
	}
	g.Paired = serverPaired{s}
	for _, pq := range qs {
		a, ok := ans[pq.Question]
		if !ok {
			continue
		}
		for _, o := range pq.Options {
			a = strings.ReplaceAll(a, o.Label+RecommendedMark, o.Label)
		}
		choices, other := choicesOf(pq, a)
		if _, err := g.Answer(caller.Caller{Kind: caller.Human}, cfg, pq.ID, choices, other); err != nil {
			s.log.Printf("program %s: answer %s: %v", slug, pq.ID, err)
		}
	}
}

// RecommendedMark follows a recommended option's label in the question
// dialog, as Claude Code's own dialogs mark one.
const RecommendedMark = " (Recommended)"

// recommendedFirst is q with its options labels first, in their order,
// then the rest.
func recommendedFirst(q project.Question, labels []string) project.Question {
	if len(labels) == 0 {
		return q
	}
	opts := make([]project.QuestionOption, 0, len(q.Options))
	for _, o := range q.Options {
		if slices.Contains(labels, o.Label) {
			opts = append(opts, o)
		}
	}
	for _, o := range q.Options {
		if !slices.Contains(labels, o.Label) {
			opts = append(opts, o)
		}
	}
	q.Options = opts
	return q
}

// serverPaired is program.Paired inside tm's server, for the human: a
// paired member's questions as the mirror has them, an answer over
// peer.need-act.
type serverPaired struct{ s *Server }

func (p serverPaired) Question(key, id string) (project.Question, error) {
	alias, member, _ := config.Remote(key)
	return program.PairedQuestion(key, id, program.MirrorNeeds(p.s.mirror.List().Peers, alias, member))
}

func (p serverPaired) Answer(key string, act proto.PeerAct) (bool, error) {
	alias, _, _ := config.Remote(key)
	c := caller.Caller{Kind: caller.Human}
	cc := callCtx{method: proto.MethodPeerNeedAct, human: true, who: func() caller.Caller { return c }}
	res, perr := p.s.needAct(proto.PeerNeedActParams{Peer: alias, PeerAct: act}, cc)
	if perr != nil {
		return false, perr
	}
	return res.(proto.PeerActResult).Asked, nil
}

// choicesOf reads a dialog's answer to q: an option's label, a
// multi-select's labels joined with ", " in the options' order, else
// the user's own words.
func choicesOf(q project.Question, a string) (choices []string, other string) {
	rest := a
	for _, o := range q.Options {
		if rest == "" {
			break
		}
		if after, ok := strings.CutPrefix(rest, o.Label); ok && (after == "" || strings.HasPrefix(after, ", ")) {
			choices = append(choices, o.Label)
			rest = strings.TrimPrefix(after, ", ")
			if !q.MultiSelect {
				break
			}
		}
	}
	if rest != "" || len(choices) == 0 {
		return nil, a
	}
	return choices, ""
}

// textClip is s cut to n runes.
func textClip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// programQuestionsText is what a director's /answer says, in the mod's
// words (view.ts).
func programQuestionsText(n int, perr *proto.Error) string {
	if perr != nil {
		return "Not asked: " + perr.Message
	}
	return fmt.Sprintf("Asked: the program's %d question(s) are open on the director's session; your answer goes to every question each covers", n)
}
