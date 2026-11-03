package program

import (
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// AskQuestion is need n, one of a coordinator's open questions as a
// paired server shows it (PeerNeed "ask:Q3"), as a stored question: its
// id, words and options, which a program's merges and similarity hints
// read.
func AskQuestion(n proto.PeerNeed) (project.Question, bool) {
	id, ok := strings.CutPrefix(n.ID, "ask:")
	if n.Kind != proto.NeedAsk || !ok || !project.IsQuestionID(id) || n.Escape || n.Question == nil || len(n.Question.Questions) != 1 {
		return project.Question{}, false
	}
	it := n.Question.Questions[0]
	q := project.Question{ID: id, Header: it.Header, Question: it.Question, MultiSelect: it.MultiSelect, Task: n.Task, Asked: n.Since}
	for _, o := range it.Options {
		q.Options = append(q.Options, project.QuestionOption{Label: o.Label, Description: o.Description})
	}
	return q, true
}

// PairedQuestion is paired member key's open question id among needs,
// what its server shows this one; unknown-question when it isn't there.
func PairedQuestion(key, id string, needs []proto.PeerNeed) (project.Question, error) {
	for _, n := range needs {
		if q, ok := AskQuestion(n); ok && q.ID == id {
			return q, nil
		}
	}
	alias, _, _ := config.Remote(key)
	return project.Question{}, refuse("unknown-question", "no open question %s in %s that %s shows this server (it shows them while its needs switch for this server is view or answer)", id, key, alias)
}

// Call is a call to tm's server: a method, its params and where its
// answer goes.
type Call func(method string, params, result any) error

// PairedVia is Paired through tm's server for c, as call reaches it,
// for program slug: the human reads a paired member's questions from
// the server's mirror (peer.list), a director from the program's
// rolled-up needs (program.needs, where a question an open program
// question covers has no row); an answer goes over peer.need-act, the
// human's.
func PairedVia(c caller.Caller, slug string, call Call) Paired {
	return pairedVia{c: c, slug: slug, call: call}
}

type pairedVia struct {
	c    caller.Caller
	slug string
	call Call
}

func (v pairedVia) Question(key, id string) (project.Question, error) {
	alias, member, ok := config.Remote(key)
	if !ok {
		return project.Question{}, refuse("invalid-question", "%s is not on a paired server", key)
	}
	var needs []proto.PeerNeed
	if v.c.IsAgent() {
		var res proto.ProgramNeedsResult
		if err := v.call(proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: v.slug}, &res); err != nil {
			return project.Question{}, err
		}
		for _, n := range res.Needs {
			if n.Server == alias && n.Project == member {
				needs = append(needs, n.PeerNeed)
				needs = append(needs, n.Also...)
			}
		}
	} else {
		var res proto.PeerList
		if err := v.call(proto.MethodPeerList, struct{}{}, &res); err != nil {
			return project.Question{}, err
		}
		needs = MirrorNeeds(res.Peers, alias, member)
	}
	return PairedQuestion(key, id, needs)
}

func (v pairedVia) Answer(key string, act proto.PeerAct) (bool, error) {
	alias, _, ok := config.Remote(key)
	if !ok {
		return false, refuse("invalid-question", "%s is not on a paired server", key)
	}
	var res proto.PeerActResult
	if err := v.call(proto.MethodPeerNeedAct, proto.PeerNeedActParams{Peer: alias, PeerAct: act}, &res); err != nil {
		return false, err
	}
	return res.Asked, nil
}

// MirrorNeeds are shared project member of paired server alias's needs
// as the mirror (peers) has them: none while that server doesn't show
// them to this one.
func MirrorNeeds(peers []proto.PeerMirror, alias, member string) []proto.PeerNeed {
	for _, p := range peers {
		if p.Alias != alias {
			continue
		}
		for _, mp := range p.Projects {
			if mp.Project == member {
				return mp.Needs
			}
		}
	}
	return nil
}
