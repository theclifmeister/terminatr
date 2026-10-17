package peer

import (
	"reflect"
	"regexp"

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
	return w
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
