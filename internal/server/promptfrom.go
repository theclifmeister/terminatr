package server

import (
	"strings"
	"sync"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// sentKeep is how many of a session's latest prompts from its
// coordinator sentPrompts keeps, and sentKey how much of a prompt it
// compares.
const (
	sentKeep = 32
	sentKey  = 240
)

// sentPrompts are the prompts sent to sessions through session.prompt
// by a project's coordinator, by session, so a conversation can say
// who sent which (transcript.watch's From). The user's own prompts and
// tm's need no record: the rest is the user's, and tm's say so.
type sentPrompts struct {
	mu sync.Mutex
	by map[string][]string
}

// add records text, sent to session id by c.
func (sp *sentPrompts) add(id string, c caller.Caller, text string) {
	if c.Kind != caller.Coordinator {
		return
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.by == nil {
		sp.by = map[string][]string{}
	}
	l := append(sp.by[id], promptKey(text))
	if over := len(l) - sentKeep; over > 0 {
		l = l[over:]
	}
	sp.by[id] = l
}

// forget drops session id's record.
func (sp *sentPrompts) forget(id string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	delete(sp.by, id)
}

// from is who sent session id the prompt text: proto.FromTM for a
// thread's or a coordinator's kickoff or a "[tm]" prompt, proto.FromCoordinator for one
// its coordinator sent, "" for the user's.
func (sp *sentPrompts) from(id, text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "[tm]") || strings.HasPrefix(t, kickoffStart) || strings.HasPrefix(t, adoptStart) ||
		t == CoordinatorKickoff {
		return proto.FromTM
	}
	k := promptKey(text)
	sp.mu.Lock()
	defer sp.mu.Unlock()
	for _, s := range sp.by[id] {
		if s == k {
			return proto.FromCoordinator
		}
	}
	return ""
}

// The first words of the prompts tm starts a thread with (a
// coordinator's, CoordinatorKickoff, is whole).
var (
	kickoffStart = cutAt(thread.Kickoff("\x00"), "\x00")
	adoptStart   = cutAt(thread.AdoptKickoff("", "\x00", ""), "\x00")
)

func cutAt(s, sep string) string {
	before, _, _ := strings.Cut(s, sep)
	return before
}

// promptKey is what of a prompt sentPrompts compares: its clean text's
// words, as far as the transcript keeps them.
func promptKey(text string) string {
	k := strings.Join(strings.Fields(textsafe.Text(text)), " ")
	if len(k) > sentKey {
		k = textsafe.Cut(k, sentKey)
	}
	return k
}
