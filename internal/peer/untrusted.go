package peer

import (
	"reflect"
	"regexp"
	"slices"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// What a paired server answers is still only its word: the text in it
// reaches this server's console, CLI and log, so it is cleaned where it
// arrives, every string of it, and names that key things here (a
// project's slug, a thread's id) are checked against their pattern.

// maxPeerText cuts any one string a peer sends, in bytes.
const maxPeerText = 4 << 10

var (
	// peerSlug is a project slug as tm makes one (project.ValidSlug).
	peerSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	// peerThread is a thread id (tasks.ThreadIDPattern).
	peerThread = regexp.MustCompile(`^t-[0-9]{4,12}$`)
	// peerMessage is a link message's id (link.ValidID).
	peerMessage = regexp.MustCompile(`^M-[0-9A-Za-z][0-9A-Za-z-]{0,39}$`)
	// peerWord is an id, a code, a state or another one-word value.
	peerWord = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/#-]{0,63}$`)
)

// cleanStrings runs every string v points to (fields, slice elements and
// map values, however deep) through textsafe.Clean, newlines kept, cut
// to maxPeerText.
func cleanStrings(v any) { cleanValue(reflect.ValueOf(v)) }

func cleanValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			cleanValue(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				cleanValue(v.Field(i))
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			cleanValue(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(v.MapIndex(k))
			cleanValue(e)
			v.SetMapIndex(k, e)
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(textsafe.Cut(textsafe.Clean(v.String(), true), maxPeerText))
		}
	}
}

// word is s when it is one word a peer may use as an id; else "".
func word(re *regexp.Regexp, s string) string {
	if re.MatchString(s) {
		return s
	}
	return ""
}

// cleanProjects is a peer.projects answer with every string cleaned and
// the projects whose slug isn't one dropped.
func cleanProjects(ps proto.PeerProjects) proto.PeerProjects {
	cleanStrings(&ps)
	ok := ps.Projects[:0:0]
	for _, p := range ps.Projects {
		if peerSlug.MatchString(p.Project) {
			p.Goal = textsafe.Line(p.Goal, 200)
			ok = append(ok, p)
		}
	}
	ps.Projects = ok
	return ps
}

// cleanWatch is a peer.watch answer with every string cleaned, the
// words a row shows on one line, and the ids it names checked: a thread
// or session whose id isn't one is dropped, other ids that aren't are
// blanked.
func cleanWatch(w proto.PeerWatch) proto.PeerWatch {
	cleanStrings(&w)
	ss := w.Sessions[:0:0]
	for _, s := range w.Sessions {
		if peerWord.MatchString(s.ID) && peerSlug.MatchString(s.Project) {
			s.Thread, s.Task = word(peerThread, s.Thread), word(peerWord, s.Task)
			s.Role, s.Agent, s.State, s.Model = textsafe.Line(s.Role, 40), textsafe.Line(s.Agent, 40), textsafe.Line(s.State, 40), textsafe.Line(s.Model, 40)
			s.Reason, s.Current = textsafe.Line(s.Reason, 200), textsafe.Line(s.Current, 200)
			ss = append(ss, s)
		}
	}
	w.Sessions = ss
	ts := w.Watch.Threads[:0:0]
	for _, t := range w.Watch.Threads {
		if peerThread.MatchString(t.ID) {
			t.Session, t.State, t.PR = word(peerWord, t.Session), textsafe.Line(t.State, 40), textsafe.Line(t.PR, 80)
			t.Title, t.Reason, t.NeedsYou = textsafe.Line(t.Title, 200), textsafe.Line(t.Reason, 200), textsafe.Line(t.NeedsYou, 300)
			if t.Task != nil {
				task := *t.Task
				task.ID, task.Status, task.Title = word(peerWord, task.ID), textsafe.Line(task.Status, 40), textsafe.Line(task.Title, 200)
				t.Task = &task
			}
			ts = append(ts, t)
		}
	}
	w.Watch.Threads = ts
	for i := range w.Watch.NeedsYou {
		n := &w.Watch.NeedsYou[i]
		n.Why, n.Task, n.Status, n.Session, n.PR = textsafe.Line(n.Why, 40), word(peerWord, n.Task), textsafe.Line(n.Status, 40), word(peerWord, n.Session), textsafe.Line(n.PR, 80)
		n.Thread = word(peerThread, n.Thread)
		n.Title = textsafe.Line(n.Title, 200)
	}
	w.Needs = cleanNeeds(w.Needs)
	return w
}

// needKinds are the kinds a PeerNeed may have: one of another kind is
// dropped.
var needKinds = []string{proto.NeedQuestion, proto.NeedApproval, proto.NeedAsk, proto.NeedReview, proto.NeedBlocked, proto.NeedCI, proto.NeedQueue}

// cleanNeeds are a peer's needs (strings cleaned already) with the ids
// they name checked and the words a row shows on one line: a need
// without an id or of another kind is dropped, and an escape's keeps no
// menu or dialog whatever the peer sent (§16.16).
func cleanNeeds(ns []proto.PeerNeed) []proto.PeerNeed {
	out := ns[:0:0]
	for _, n := range ns {
		if !peerWord.MatchString(n.ID) || !slices.Contains(needKinds, n.Kind) {
			continue
		}
		n.Session, n.Thread, n.Task = word(peerWord, n.Session), word(peerThread, n.Thread), word(peerWord, n.Task)
		n.Role, n.Status, n.PR, n.Asked = textsafe.Line(n.Role, 40), textsafe.Line(n.Status, 40), textsafe.Line(n.PR, 80), textsafe.Line(n.Asked, 40)
		n.Title = textsafe.Line(n.Title, 200)
		if n.Escape {
			n.Question, n.Approval = nil, nil
		}
		if q := n.Question; q != nil {
			c := *q
			c.Tool = textsafe.Line(c.Tool, 40)
			c.Questions = slices.Clone(c.Questions)
			for i := range c.Questions {
				it := &c.Questions[i]
				it.Header, it.Answer = textsafe.Line(it.Header, 40), textsafe.Line(it.Answer, 300)
				it.Options = slices.Clone(it.Options)
				for j := range it.Options {
					it.Options[j].Label = textsafe.Line(it.Options[j].Label, 200)
				}
			}
			n.Question = &c
		}
		if a := n.Approval; a != nil {
			c := cleanApproval(*a)
			n.Approval = &c
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cleanApproval is a dialog with each option and scope on one line.
func cleanApproval(a proto.PeerApproval) proto.PeerApproval {
	a.Options, a.Scopes = slices.Clone(a.Options), slices.Clone(a.Scopes)
	for i := range a.Options {
		a.Options[i] = textsafe.Line(a.Options[i], 300)
	}
	for i := range a.Scopes {
		a.Scopes[i] = textsafe.Line(a.Scopes[i], 20)
	}
	return a
}

// cleanError is a peer's error answer in one short line: its code when
// it is one word, else ErrRefused.
func cleanError(e *proto.Error) *proto.Error {
	code := word(peerWord, e.Code)
	if code == "" {
		code = proto.ErrRefused
	}
	return &proto.Error{Code: code, Message: textsafe.Line(e.Message, maxErrorRunes)}
}
