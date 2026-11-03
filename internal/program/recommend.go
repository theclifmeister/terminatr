package program

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
)

// The director's recommendations (T982): for an open question in the
// program's Needs you, a member coordinator's or one of the program's
// own, the director may say which answer it recommends and why, from
// what it knows across the program. A recommendation is never an
// answer: only the user answers, by accepting it (one key or tap,
// Accept) or by picking another as before. Only the program's own
// director writes one (tm program recommend), journaled; it is kept
// here, in the program's folder, keyed by the question as tm names it
// (acme-api/Q3, mini/acme-ios/Q3, or <program>/Q2), with the question's
// fingerprint (Sum): once the question is answered, closed or changed,
// the recommendation is dropped and never shown.

// RecommendFile is the program's recommendations, a JSON object by key.
const RecommendFile = "recommendations.json"

// MaxWhy is the longest reason a recommendation gives, in runes: one
// line.
const MaxWhy = 200

// Recommendation is the director's recommended answer to one open
// question: Labels, the options it recommends (one, or a
// multi-select's), or Text, an answer in its own words; Why, its one
// line reason; At, when it said so; Sum, the question it read
// (Fingerprint).
type Recommendation struct {
	Labels []string  `json:"labels,omitempty"`
	Text   string    `json:"text,omitempty"`
	Why    string    `json:"why"`
	At     time.Time `json:"at"`
	Sum    string    `json:"sum"`
}

// Words are the recommended answer as tm says it: "Main", "Yes, No",
// "Other: …".
func (r Recommendation) Words() string {
	return project.Answer{Labels: r.Labels, Other: r.Text}.Words()
}

// Fingerprint is question q as a recommendation reads it: its words,
// options and whether it is a multi-select. A question asked again in
// other words, or with other options, has another.
func Fingerprint(q project.Question) string {
	h := sha256.New()
	fmt.Fprintf(h, "%q\x00%v\x00", strings.Join(strings.Fields(q.Question), " "), q.MultiSelect)
	for _, o := range q.Options {
		fmt.Fprintf(h, "%q\x00%q\x00", o.Label, o.Description)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Recommendations are the program's recommendations by key, as stored:
// stale ones too (Current checks one against its question).
func (p *Program) Recommendations() (map[string]Recommendation, error) {
	data, err := os.ReadFile(p.Path(RecommendFile))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Recommendation{}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeRecs(data)
}

func decodeRecs(data []byte) (map[string]Recommendation, error) {
	m := map[string]Recommendation{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", RecommendFile, err)
	}
	return m, nil
}

// updateRecs is a locked read-modify-write of the store.
func (p *Program) updateRecs(fn func(m map[string]Recommendation) error) error {
	return mdfile.Update(p.Path(RecommendFile), func(old []byte) ([]byte, error) {
		m, err := decodeRecs(old)
		if err != nil {
			return nil, err
		}
		if err := fn(m); err != nil {
			return nil, err
		}
		b, err := json.MarshalIndent(m, "", "  ")
		return append(b, '\n'), err
	})
}

// Current is r while it still reads question q (the same Sum); false
// once q changed.
func Current(r Recommendation, ok bool, q project.Question) (Recommendation, bool) {
	if !ok || r.Sum != Fingerprint(q) {
		return Recommendation{}, false
	}
	return r, true
}

// asked is an open question as a recommendation names it: its Key
// (acme-api/Q3, mini/acme-ios/Q3, <program>/Q2), its member as the
// program lists it ("" for the program's own), the question now, and a
// local member's project.
type asked struct {
	Key    string
	Member string
	Q      project.Question
	child  *project.Project
}

// own reports whether a is one of the program's own questions.
func (a asked) own() bool { return a.Member == "" }

// id is a's question id: Q3.
func (a asked) id() string { return a.Q.ID }

// questionAt reads ref, an open question of the program's Needs you: Q2
// or <program>/Q2, one of its own; acme-api/Q3 or Q3@acme-api a local
// member coordinator's; mini/acme-ios/Q3 a paired one's, as its server
// shows it here.
func (p *Program) questionAt(cfg *config.Config, ref string) (asked, error) {
	ref = strings.TrimSpace(ref)
	id := ref
	if rest, ok := strings.CutPrefix(ref, p.Slug+"/"); ok {
		id = rest
	}
	if strings.HasPrefix(id, "q") {
		id = "Q" + id[1:]
	}
	if project.IsQuestionID(id) {
		q, err := p.Question(id)
		if err != nil {
			return asked{}, err
		}
		return asked{Key: p.Slug + "/" + q.ID, Q: q}, nil
	}
	slug, qid, err := MemberQuestionRef(ref)
	if err != nil {
		return asked{}, refuse("invalid-question", "%q is not an open question of the program's Needs you: Q2 (the program's), acme-api/Q3 or mini/acme-ios/Q3", ref)
	}
	a := asked{Key: slug + "/" + qid, Member: slug}
	if IsRemote(slug) {
		if a.Q, err = p.pairedQuestion(cfg, slug, qid); err != nil {
			return asked{}, err
		}
		return a, nil
	}
	if a.child, err = p.Member(cfg, slug); err != nil {
		return asked{}, err
	}
	if a.Q, err = a.child.Question(qid); err != nil {
		return asked{}, err
	}
	if a.Q.CoveredBy != "" {
		return asked{}, refuse("covered", "%s is covered by %s: it is answered at program level, there", a.Key, a.Q.CoveredBy)
	}
	items, err := a.child.Inbox()
	if err != nil {
		return asked{}, err
	}
	if project.QuestionAnswered(items, qid) {
		return asked{}, refuse("answered", "the user answered %s already; it waits on its coordinator", a.Key)
	}
	return a, nil
}

// RecommendRequest is tm program recommend's: the question (as
// questionAt reads it), the options recommended (a number, 1-based, or
// a label; several for a multi-select) or an answer in the director's
// own words, and why, on one line.
type RecommendRequest struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices,omitempty"`
	Text     string   `json:"text,omitempty"`
	Why      string   `json:"why"`
}

// mayRecommend refuses anyone but the program's own director: never the
// human (a recommendation is the director's view, the user's answer is
// theirs), another program's director, a coordinator, or a paired
// server's director.
func (p *Program) mayRecommend(c caller.Caller) error {
	switch {
	case c.Kind == caller.Director && c.Project == p.Slug:
		return nil
	case c.Kind == caller.Director:
		return refuse("other-program", "the director of %s can't recommend in program %s", c.Project, p.Slug)
	}
	return refuse("director-only", "only program %s's director recommends an answer; the user answers", p.Slug)
}

// Recommend stores the director's recommended answer to open question
// req.Question as c, the program's own director: an option (or a
// multi-select's options) it has, or an answer in words, with a one
// line reason. It answers nothing. A recommendation again replaces the
// one before. Journaled in the program.
func (p *Program) Recommend(c caller.Caller, cfg *config.Config, req RecommendRequest) (string, Recommendation, error) {
	if err := p.mayRecommend(c); err != nil {
		return "", Recommendation{}, err
	}
	if err := p.writable(cfg, "recommending"); err != nil {
		return "", Recommendation{}, err
	}
	why := strings.Join(strings.Fields(req.Why), " ")
	switch n := utf8.RuneCountInString(why); {
	case n == 0:
		return "", Recommendation{}, refuse("invalid-recommendation", "say why, in one line (--why): the facts, and where they come from")
	case n > MaxWhy:
		return "", Recommendation{}, refuse("invalid-recommendation", "the reason is %d characters; at most %d, one line", n, MaxWhy)
	}
	a, err := p.questionAt(cfg, req.Question)
	if err != nil {
		return "", Recommendation{}, err
	}
	ans, err := a.Q.Pick(req.Choices, req.Text)
	if err != nil {
		return "", Recommendation{}, err
	}
	r := Recommendation{Labels: ans.Labels, Text: ans.Other, Why: why, At: now().UTC().Truncate(time.Second), Sum: Fingerprint(a.Q)}
	if err := p.updateRecs(func(m map[string]Recommendation) error {
		m[a.Key] = r
		return nil
	}); err != nil {
		return "", Recommendation{}, err
	}
	_, _ = p.Prune(cfg)
	return a.Key, r, p.Journal(c, "program.recommend", a.Key, r.Words()+" — "+why)
}

// Unrecommend takes the director's recommendation on ref back, as c,
// the program's own director. Journaled; none there refuses
// no-recommendation.
func (p *Program) Unrecommend(c caller.Caller, ref string) (string, error) {
	if err := p.mayRecommend(c); err != nil {
		return "", err
	}
	key, err := p.recKey(ref)
	if err != nil {
		return "", err
	}
	if err := p.updateRecs(func(m map[string]Recommendation) error {
		if _, ok := m[key]; !ok {
			return refuse("no-recommendation", "the director recommends nothing on %s", key)
		}
		delete(m, key)
		return nil
	}); err != nil {
		return "", err
	}
	return key, p.Journal(c, "program.recommend.clear", key, "")
}

// recKey is ref's key, as questionAt names it, without reading the
// question (it may be closed by now).
func (p *Program) recKey(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	id := strings.TrimPrefix(ref, p.Slug+"/")
	if strings.HasPrefix(id, "q") {
		id = "Q" + id[1:]
	}
	if project.IsQuestionID(id) {
		return p.Slug + "/" + id, nil
	}
	slug, qid, err := MemberQuestionRef(ref)
	if err != nil {
		return "", err
	}
	return slug + "/" + qid, nil
}

// drop removes the recommendations on keys, journaled with why.
func (p *Program) drop(why string, keys ...string) {
	var gone []string
	_ = p.updateRecs(func(m map[string]Recommendation) error {
		for _, k := range keys {
			if _, ok := m[k]; ok {
				delete(m, k)
				gone = append(gone, k)
			}
		}
		return nil
	})
	for _, k := range gone {
		_ = p.Journal(caller.Caller{Kind: caller.Ticker}, "program.recommend.drop", k, why)
	}
}

// forget removes the recommendation on key, unjournaled: what the
// caller journals says why.
func (p *Program) forget(key string) {
	_ = p.updateRecs(func(m map[string]Recommendation) error {
		delete(m, key)
		return nil
	})
}

// Prune drops the recommendations whose question is no longer open as
// recommended: the program's own closed or changed; a local member's
// closed, changed, answered or covered by a program question (its own
// recommendation stands for it then). A paired member's is kept: its
// question is checked when shown (Current) and when accepted. It
// returns the keys dropped.
func (p *Program) Prune(cfg *config.Config) ([]string, error) {
	recs, err := p.Recommendations()
	if err != nil || len(recs) == 0 {
		return nil, err
	}
	var stale []string
	for key, r := range recs {
		member, id, ok := p.splitKey(key)
		if !ok {
			stale = append(stale, key)
			continue
		}
		if IsRemote(member) {
			continue
		}
		var q project.Question
		if member == "" {
			q, err = p.Question(id)
		} else {
			var child *project.Project
			if child, err = p.Member(cfg, member); err == nil {
				if q, err = child.Question(id); err == nil && q.CoveredBy == "" {
					items, _ := child.Inbox()
					if project.QuestionAnswered(items, id) {
						err = refuse("answered", "answered")
					}
				} else if err == nil {
					err = refuse("covered", "covered")
				}
			}
		}
		if err != nil || r.Sum != Fingerprint(q) {
			stale = append(stale, key)
		}
	}
	slices.Sort(stale)
	if len(stale) > 0 {
		p.drop("its question was answered, closed or changed", stale...)
	}
	return stale, nil
}

// splitKey is key's member ("" for the program's own) and question id.
func (p *Program) splitKey(key string) (member, id string, ok bool) {
	i := strings.LastIndex(key, "/")
	if i < 0 {
		return "", "", false
	}
	member, id = key[:i], key[i+1:]
	if !project.IsQuestionID(id) {
		return "", "", false
	}
	if member == p.Slug {
		return "", id, true
	}
	return member, id, true
}

// AcceptResult is what Accept did: the question it answered (Key), the
// answer's words, and where it went: the program's own question's
// covered members (Fanned), else the one member question's Result
// (answered, or waiting: an earlier answer waits on its coordinator).
type AcceptResult struct {
	Key    string   `json:"key"`
	Answer string   `json:"answer"`
	Result string   `json:"result,omitempty"`
	Fanned []Fanned `json:"fanned,omitempty"`
}

// Accept answers open question ref with the director's recommendation
// on it, as c, the user (one key or tap): the user's own answer, as tm
// ask answer (or program.answer, for the program's own) gives it,
// journaled as theirs, accepted from the director. Never an agent's: a
// director can't accept its own recommendation. A recommendation whose
// question changed since is dropped and refused
// (recommendation-stale); none refuses no-recommendation.
func (p *Program) Accept(c caller.Caller, cfg *config.Config, ref string) (AcceptResult, error) {
	if c.IsAgent() || c.Kind == caller.Peer {
		return AcceptResult{}, refuse("human-only", "only the user accepts the director's recommendation: it answers as theirs")
	}
	if err := p.writable(cfg, "answering"); err != nil {
		return AcceptResult{}, err
	}
	key, err := p.recKey(ref)
	if err != nil {
		return AcceptResult{}, err
	}
	recs, err := p.Recommendations()
	if err != nil {
		return AcceptResult{}, err
	}
	stored, ok := recs[key]
	if !ok {
		return AcceptResult{}, refuse("no-recommendation", "the director recommends nothing on %s", key)
	}
	a, err := p.questionAt(cfg, key)
	if err != nil {
		p.drop("its question was answered, closed or changed", key)
		return AcceptResult{}, err
	}
	r, ok := Current(stored, true, a.Q)
	if !ok {
		p.drop("its question changed", key)
		return AcceptResult{}, refuse("recommendation-stale", "%s changed since the director recommended an answer: look again", key)
	}
	hc := c
	hc.Via = "program " + p.Slug + ", the director's recommendation"
	if c.Via != "" {
		hc.Via = c.Via + " › " + hc.Via
	}
	res := AcceptResult{Key: key, Answer: r.Words()}
	switch {
	case a.own():
		ar, err := p.Answer(hc, cfg, a.id(), r.Labels, r.Text)
		res.Fanned = ar.Fanned
		if err != nil && len(ar.Fanned) == 0 {
			return res, err
		}
	case IsRemote(a.Member):
		res.Result, _, err = p.fanOutPaired(cfg, project.Cover{Project: a.Member, Question: a.id()}, r.Labels, r.Text)
		if err != nil {
			return res, err
		}
	default:
		res.Result, _, err = fanOut(a.child, hc, a.id(), r.Labels, r.Text)
		if err != nil {
			return res, err
		}
	}
	p.forget(key)
	return res, p.Journal(hc, "program.recommend.accept", key, r.Words())
}
