package program

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Program questions (the programs design §7.1, §7.2): the server
// suggests which of the members' open questions ask the same thing
// (Similar); the director merges them into one question of the
// program's (Merge), which covers each; the user answers that once
// (Answer), and the answer goes to every covered question as the user's
// own answer there, with a program-answer item for the director.

// KindAnswer is the item in the program's inbox once the user answered
// one of its questions: what they picked and where it went, for the
// director to follow up. Fixed words, but for the user's own.
const KindAnswer = "program-answer"

// SimilarAt is the Jaccard similarity of two questions' normalised
// words at which the server flags them as alike.
const SimilarAt = 0.6

// MaxMerge is the most member questions one program question covers.
const MaxMerge = 8

// stopWords are left out of a question's words: they say nothing of
// what it asks.
var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the and or but if of to in on at by for with from as is are was were be been
		being do does did doing have has had having it its this that these those there here we you your our us i me my
		he she they them their what which who whom whose when where why how should shall would will can could may might
		must not no yes so than then too very just about into over under any all some each per via vs`) {
		stopWords[w] = true
	}
}

// Words are text's words as similarity compares them: lower case,
// split at what is neither letter nor digit, stop words out, each
// stemmed (stem).
func Words(text string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if stopWords[w] {
			continue
		}
		out = append(out, stem(w))
	}
	return out
}

// stem cuts a few English suffixes off w, so "deploys", "deployed" and
// "deploying" compare as one word. Short words stay as they are.
func stem(w string) string {
	for _, suf := range []string{"ing", "ed", "es", "s"} {
		if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 3 {
			w = strings.TrimSuffix(w, suf)
			break
		}
	}
	return w
}

// Jaccard is the similarity of two word lists as sets: what they share
// over what either has. 0 when both are empty.
func Jaccard(a, b []string) float64 {
	sa, sb := map[string]bool{}, map[string]bool{}
	for _, w := range a {
		sa[w] = true
	}
	for _, w := range b {
		sb[w] = true
	}
	inter := 0
	for w := range sa {
		if sb[w] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// label is an option's label as two questions' options are matched:
// its words, lower case, joined by one space.
func label(l string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(l), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// OptionMap maps a's options onto b's: for each option of a, b's label
// that matches it after normalising (label), in a's order. False when
// they don't match one to one, or one is a multi-select and the other
// not.
func OptionMap(a, b project.Question) ([]string, bool) {
	if len(a.Options) != len(b.Options) || a.MultiSelect != b.MultiSelect {
		return nil, false
	}
	out := make([]string, len(a.Options))
	used := make([]bool, len(b.Options))
	for i, o := range a.Options {
		j := slices.IndexFunc(b.Options, func(bo project.QuestionOption) bool { return label(bo.Label) == label(o.Label) })
		if j < 0 || used[j] {
			return nil, false
		}
		used[j], out[i] = true, b.Options[j].Label
	}
	return out, true
}

// Similar reports whether a and b ask alike enough for the server to
// suggest merging them: their words' Jaccard similarity at SimilarAt or
// over, and options that match (OptionMap).
func Similar(a, b project.Question) bool {
	if _, ok := OptionMap(a, b); !ok {
		return false
	}
	return Jaccard(Words(a.Question), Words(b.Question)) >= SimilarAt
}

// MemberQuestionRef reads a member's question as tm program ask takes
// it: Q3@acme-api, or acme-api/Q3; a paired member's Q3@mini/acme-ios,
// or mini/acme-ios/Q3 (slug is then mini/acme-ios).
func MemberQuestionRef(ref string) (slug, id string, err error) {
	ref = strings.TrimSpace(ref)
	q, slug, ok := strings.Cut(ref, "@")
	if !ok {
		var i int
		if i = strings.LastIndex(ref, "/"); i >= 0 {
			slug, q, ok = ref[:i], ref[i+1:], true
		}
	}
	q, slug = strings.TrimSpace(q), strings.TrimSpace(slug)
	if strings.HasPrefix(q, "q") {
		q = "Q" + q[1:]
	}
	if !ok || !project.IsQuestionID(q) {
		return "", "", refuse("invalid-question", "%q is not a member's question like Q3@acme-api", ref)
	}
	if strings.Contains(slug, "/") && !IsRemote(slug) {
		return "", "", refuse("invalid-question", "%q is not a member's question like Q3@acme-api or Q3@mini/acme-ios", ref)
	}
	return slug, q, nil
}

// Paired reaches the coordinators' questions of a program's members on
// paired servers (the programs design §7.2), through tm's server: what
// their servers show it at needs view or answer, and peer.act at
// answer. A Program without it (nil) refuses such questions
// (remote-member).
type Paired interface {
	// Question is paired member key's ("mini/acme-ios") open
	// coordinator question id as its server last showed it; an
	// unknown-question refusal once it isn't open there.
	Question(key, id string) (project.Question, error)
	// Answer sends the user's answer to one of them (proto.AnswersAsk):
	// asked is false when an earlier answer waits on its coordinator.
	Answer(key string, act proto.PeerAct) (asked bool, err error)
}

// pairedQuestion is paired member key's open question id, for a merge:
// key is p's member, and p reaches paired servers.
func (p *Program) pairedQuestion(cfg *config.Config, key, id string) (project.Question, error) {
	if cfg.ProgramOf(key) != p.Slug {
		return project.Question{}, refuse("not-member", "%s is not a member of program %s (tm program show %s)", key, p.Slug, p.Slug)
	}
	if p.Paired == nil {
		return project.Question{}, refuse("remote-member", "%s is on a paired server: its questions are read through tm's server, which isn't reached here", key)
	}
	return p.Paired.Question(key, id)
}

// MergeRequest is tm program ask merge's: the member questions, and
// optionally the program question's own words and, per member question
// whose options don't match the first's by label, which of its options
// each of the first's options is (1-based: "2,1,3").
type MergeRequest struct {
	Questions []string          `json:"questions"`
	Text      string            `json:"text,omitempty"`
	Header    string            `json:"header,omitempty"`
	Map       map[string]string `json:"map,omitempty"`
}

// Merge makes program question Qn out of req's member questions as c,
// the program's director or the human (§7.2): each must be open in a
// local member, covered by no other program question and not answered
// yet, with options that match the first's (OptionMap) or a map for
// it. The program question takes the first's options, its words unless
// req gives its own, and covers them all; each member question is
// marked covered by it. Journaled in the program and each member.
func (p *Program) Merge(c caller.Caller, cfg *config.Config, req MergeRequest) (project.Question, error) {
	if err := p.mayDirect(c, "merging questions"); err != nil {
		return project.Question{}, err
	}
	if err := p.writable(cfg, "merging questions"); err != nil {
		return project.Question{}, err
	}
	if n := len(req.Questions); n < 2 || n > MaxMerge {
		return project.Question{}, refuse("invalid-merge", "a merge takes 2 to %d member questions, not %d", MaxMerge, n)
	}
	// A paired member's question can't be marked covered on its
	// server: the program's open questions say what they cover.
	open, err := p.Questions()
	if err != nil {
		return project.Question{}, err
	}
	coveredBy := map[string]string{}
	for _, o := range open {
		for _, cv := range o.Covers {
			coveredBy[cv.Ref()] = p.Slug + "/" + o.ID
		}
	}
	type member struct {
		key   string           // the member as the program lists it
		child *project.Project // nil on a paired server
		q     project.Question
	}
	var ms []member
	seen := map[string]bool{}
	for _, ref := range req.Questions {
		slug, id, err := MemberQuestionRef(ref)
		if err != nil {
			return project.Question{}, err
		}
		if seen[slug+"/"+id] {
			return project.Question{}, refuse("invalid-merge", "%s/%s is named twice", slug, id)
		}
		seen[slug+"/"+id] = true
		if IsRemote(slug) {
			q, err := p.pairedQuestion(cfg, slug, id)
			if err != nil {
				return project.Question{}, err
			}
			if by := coveredBy[slug+"/"+id]; by != "" {
				return project.Question{}, refuse("covered", "%s/%s is covered by %s already", slug, id, by)
			}
			ms = append(ms, member{key: slug, q: q})
			continue
		}
		child, err := p.Member(cfg, slug)
		if err != nil {
			return project.Question{}, err
		}
		q, err := child.Question(id)
		if err != nil {
			return project.Question{}, err
		}
		if q.CoveredBy != "" {
			return project.Question{}, refuse("covered", "%s/%s is covered by %s already", slug, id, q.CoveredBy)
		}
		items, err := child.Inbox()
		if err != nil {
			return project.Question{}, err
		}
		if project.QuestionAnswered(items, id) {
			return project.Question{}, refuse("answered", "the user answered %s/%s already; it waits on its coordinator", slug, id)
		}
		ms = append(ms, member{key: slug, child: child, q: q})
	}
	first := ms[0].q
	pq := project.Question{
		Header:      strings.TrimSpace(req.Header),
		Question:    strings.TrimSpace(req.Text),
		Options:     slices.Clone(first.Options),
		MultiSelect: first.MultiSelect,
		Recommended: first.Recommended,
	}
	if pq.Question == "" {
		pq.Question = first.Question
	}
	if pq.Header == "" {
		pq.Header = first.Header
		if first.Header == first.Task {
			pq.Header = "" // a member's task means nothing at program level
		}
	}
	for _, m := range ms {
		ref := m.key + "/" + m.q.ID
		mapping, ok := OptionMap(first, m.q)
		if spec, given := mapFor(req.Map, m.key, m.q.ID); given {
			var err error
			if mapping, err = parseMap(spec, ref, first, m.q); err != nil {
				return project.Question{}, err
			}
		} else if !ok {
			return project.Question{}, refuse("incompatible", "%s's options don't match %s/%s's (%s): give a map for it, --map %s=%s",
				ref, ms[0].key, first.ID, optionWords(first), ref, identity(len(first.Options)))
		}
		pq.Covers = append(pq.Covers, project.Cover{Project: m.key, Question: m.q.ID, Map: mapping})
	}
	if pq.Recommended != "" {
		// Kept only when every member recommends the same option.
		for _, m := range ms {
			i := slices.IndexFunc(first.Options, func(o project.QuestionOption) bool { return o.Label == pq.Recommended })
			cv := pq.Covers[slices.IndexFunc(pq.Covers, func(cv project.Cover) bool { return cv.Project == m.key && cv.Question == m.q.ID })]
			if i < 0 || cv.Map[i] != m.q.Recommended {
				pq.Recommended = ""
				break
			}
		}
	}
	if i := slices.IndexFunc(open, func(o project.Question) bool { return o.Question == pq.Question && o.Task == "" }); i >= 0 {
		return project.Question{}, refuse("merged", "%s/%s asks that already: give it other words (--text)", p.Slug, open[i].ID)
	}
	got, err := p.AddQuestion(pq)
	if err != nil {
		return project.Question{}, err
	}
	by := p.Slug + "/" + got.ID
	var marked []member
	for _, m := range ms {
		if m.child == nil {
			continue // a paired member's: the program question covers it
		}
		if err := m.child.SetCoveredBy(m.q.ID, by, ""); err != nil {
			// Another merge took it meanwhile: undo this one.
			for _, done := range marked {
				_ = done.child.SetCoveredBy(done.q.ID, "", by)
			}
			_, _ = p.DoneQuestion(got.ID)
			return project.Question{}, err
		}
		marked = append(marked, m)
		if err := m.child.Journal(c, "program.ask.merge", m.q.ID, "covered by "+by); err != nil {
			return got, err
		}
	}
	return got, p.Journal(c, "program.ask.merge", got.ID, "covers "+coverRefs(got.Covers))
}

// mapFor is req's map for member question slug/id, by either name.
func mapFor(m map[string]string, slug, id string) (string, bool) {
	for _, k := range []string{slug + "/" + id, id + "@" + slug} {
		if v, ok := m[k]; ok {
			return v, true
		}
	}
	return "", false
}

// parseMap reads a map given for member question q of ref ("2,1,3": for
// each of first's options, the number of q's option it is).
func parseMap(spec, ref string, first, q project.Question) ([]string, error) {
	parts := strings.Split(spec, ",")
	if len(parts) != len(first.Options) {
		return nil, refuse("invalid-map", "%s's map names %d options; the program question has %d", ref, len(parts), len(first.Options))
	}
	if first.MultiSelect != q.MultiSelect {
		return nil, refuse("incompatible", "%s is a multi-select and the other isn't, or the other way round", ref)
	}
	out := make([]string, len(parts))
	used := map[int]bool{}
	for i, s := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 1 || n > len(q.Options) || used[n] {
			return nil, refuse("invalid-map", "%s's map: %q is not one of its options 1 to %d, each once", ref, s, len(q.Options))
		}
		used[n], out[i] = true, q.Options[n-1].Label
	}
	return out, nil
}

// optionWords are q's options numbered, for a refusal: "1 Yes, 2 No".
func optionWords(q project.Question) string {
	var out []string
	for i, o := range q.Options {
		out = append(out, fmt.Sprintf("%d %s", i+1, oneLine(o.Label, 30)))
	}
	return strings.Join(out, ", ")
}

// identity is the map "1,2,…,n".
func identity(n int) string {
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, strconv.Itoa(i))
	}
	return strings.Join(out, ",")
}

// coverRefs are covers by name: "acme-api/Q3, acme-web/Q7".
func coverRefs(cs []project.Cover) string {
	refs := make([]string, len(cs))
	for i, c := range cs {
		refs[i] = c.Ref()
	}
	return strings.Join(refs, ", ")
}

// Fanned is what came of the user's answer in one covered question.
type Fanned struct {
	Project  string `json:"project"`
	Question string `json:"question"`
	// Result is answered, waiting (an earlier answer to it waits on
	// its coordinator), closed (no longer open there) or failed.
	Result string `json:"result"`
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

// AnswerResult is what Answer did.
type AnswerResult struct {
	Question string         `json:"question"`
	Answer   project.Answer `json:"answer"`
	Fanned   []Fanned       `json:"fanned"`
}

// Answer records the user's answer to the program's open question ref
// (choices and other as tm ask answer takes them) and sends it to each
// question it covers, mapped to that question's options, as the user's
// answer there: each member gets its usual answer item, journaled as
// "human (program <slug>)". The program question then closes, with a
// program-answer item for the director and a journal line. Only the
// user answers (human-only): never the director, which can't relay it.
func (p *Program) Answer(c caller.Caller, cfg *config.Config, ref string, choices []string, other string) (AnswerResult, error) {
	if c.IsAgent() {
		return AnswerResult{}, refuse("human-only", "only the user answers a program's question (the director's /answer dialog, or tm ask answer %s/Q2 in their own terminal)", p.Slug)
	}
	q, err := p.Question(ref)
	if err != nil {
		return AnswerResult{}, err
	}
	a, err := q.Pick(choices, other)
	if err != nil {
		return AnswerResult{}, err
	}
	res := AnswerResult{Question: q.ID, Answer: a, Fanned: []Fanned{}}
	via := "program " + p.Slug
	if c.Via != "" {
		via = c.Via + " › " + via
	}
	hc := c
	hc.Via = via
	by := p.Slug + "/" + q.ID
	for _, cv := range q.Covers {
		f := Fanned{Project: cv.Project, Question: cv.Question}
		labels, err := mapped(q, cv, a.Labels)
		if err == nil && IsRemote(cv.Project) {
			f.Result, f.Answer, err = p.fanOutPaired(cfg, cv, labels, a.Other)
		} else if err == nil {
			var child *project.Project
			if child, err = p.Member(cfg, cv.Project); err == nil {
				f.Result, f.Answer, err = fanOut(child, hc, cv.Question, labels, a.Other)
				// The covered question is the member's own again once
				// answered: its coordinator closes it as any other.
				if f.Result != "closed" {
					_ = child.SetCoveredBy(cv.Question, "", by)
				}
			}
		}
		if err != nil {
			f.Result, f.Error = "failed", err.Error()
		}
		res.Fanned = append(res.Fanned, f)
	}
	if _, err := p.DoneQuestion(q.ID); err != nil {
		return res, err
	}
	var where []string
	for _, f := range res.Fanned {
		where = append(where, f.Project+"/"+f.Question+" "+f.Result)
	}
	words := fmt.Sprintf("the user answered %s: %s; sent to %s", by, a.Words(), strings.Join(where, ", "))
	if _, err := p.AddItem(KindAnswer, q.ID, words, false); err != nil {
		return res, err
	}
	return res, p.Journal(hc, "program.answer", q.ID, a.Words()+" → "+strings.Join(where, ", "))
}

// mapped are labels, the program question q's options picked, as cover
// cv's question names them.
func mapped(q project.Question, cv project.Cover, labels []string) ([]string, error) {
	var out []string
	for _, l := range labels {
		i := slices.IndexFunc(q.Options, func(o project.QuestionOption) bool { return o.Label == l })
		if i < 0 || i >= len(cv.Map) {
			return nil, refuse("invalid-map", "%s has no option for %q", cv.Ref(), l)
		}
		out = append(out, cv.Map[i])
	}
	return out, nil
}

// fanOut answers child's question id as c with labels and other: its
// result, and the answer's words there.
func fanOut(child *project.Project, c caller.Caller, id string, labels []string, other string) (string, string, error) {
	if _, err := child.Question(id); err != nil {
		if e, ok := err.(*project.Error); ok && e.Code == "unknown-question" {
			return "closed", "", nil
		}
		return "", "", err
	}
	_, a, asked, err := child.AnswerQuestion(c, id, labels, other)
	if err != nil {
		return "", "", err
	}
	if !asked {
		return "waiting", a.Words(), nil
	}
	return "answered", a.Words(), nil
}

// fanOutPaired sends the user's answer, labels and other, to covered
// question cv of a paired member over peer.act, as fanOut does a local
// one's: its result and the answer's words there. The question is read
// again first, so the act names it as its server shows it now.
func (p *Program) fanOutPaired(cfg *config.Config, cv project.Cover, labels []string, other string) (string, string, error) {
	q, err := p.pairedQuestion(cfg, cv.Project, cv.Question)
	if err != nil {
		if e, ok := err.(*project.Error); ok && e.Code == "unknown-question" {
			return "closed", "", nil
		}
		return "", "", err
	}
	a, err := q.Pick(labels, other)
	if err != nil {
		return "", "", err
	}
	_, slug, _ := config.Remote(cv.Project)
	act := proto.PeerAct{Project: slug, Need: "ask:" + q.ID, Act: proto.ActAnswer, Question: q.Question, Other: a.Other}
	if q.MultiSelect {
		for _, l := range a.Labels {
			act.Picks = append(act.Picks, slices.IndexFunc(q.Options, func(o project.QuestionOption) bool { return o.Label == l }))
		}
		act.Answer = strings.Join(a.Labels, ", ")
	} else if len(a.Labels) > 0 {
		act.Answer = a.Labels[0]
	}
	asked, err := p.Paired.Answer(cv.Project, act)
	if err != nil {
		return "", "", err
	}
	if !asked {
		return "waiting", a.Words(), nil
	}
	return "answered", a.Words(), nil
}

// Withdraw closes the program's open question ref unanswered as c, the
// director or the human: no longer asked. The member questions it
// covered are their own again (their mark cleared), still open there.
func (p *Program) Withdraw(c caller.Caller, cfg *config.Config, ref string) (project.Question, error) {
	if err := p.mayDirect(c, "closing a program question"); err != nil {
		return project.Question{}, err
	}
	q, err := p.DoneQuestion(ref)
	if err != nil {
		return q, err
	}
	by := p.Slug + "/" + q.ID
	for _, cv := range q.Covers {
		if IsRemote(cv.Project) {
			continue // marked nowhere but here
		}
		if child, err := p.Member(cfg, cv.Project); err == nil {
			if child.SetCoveredBy(cv.Question, "", by) == nil {
				_ = child.Journal(c, "program.ask.done", cv.Question, "no longer covered by "+by)
			}
		}
	}
	return q, p.Journal(c, "program.ask.done", q.ID, "")
}
