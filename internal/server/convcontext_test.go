package server

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/version"
)

// convServer is a server with thread session s-1 (with the mod) on its
// current build.
func convServer() *Server {
	s := &Server{log: log.New(os.Stderr, "", 0)}
	s.records = map[string]SessionRecord{
		"s-1": {ID: "s-1", Role: proto.RoleThread, Brief: "/p/threads/t-1/brief.md", ContextBuild: version.BuildID()},
	}
	return s
}

// started is session s-1's SessionStart answer for source.
func started(t *testing.T, s *Server, src string) string {
	t.Helper()
	b, err := s.hookContextOf("s-1", true)(map[string]any{"hook_event_name": "SessionStart", "source": src})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// prompted is session s-1's UserPromptSubmit answer.
func prompted(t *testing.T, s *Server, mod bool) string {
	t.Helper()
	b, err := s.hookContextOf("s-1", mod)(map[string]any{"hook_event_name": "UserPromptSubmit"})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestContextMarked: with the mod's hooks running, SessionStart says
// where the context is and UserPromptSubmit adds nothing, whether Claude
// reads the blocks before or after the first prompt's hook.
func TestContextMarked(t *testing.T) {
	s := convServer()
	s.markedStart("s-1")
	if got := started(t, s, "startup"); got != modContextLine {
		t.Fatalf("startup: %q", got)
	}
	if got := prompted(t, s, true); got != "" {
		t.Fatalf("first prompt, block due: %.80q", got)
	}
	if !s.blockContext("s-1") {
		t.Fatal("the block was refused")
	}
	for range 3 {
		if got := prompted(t, s, true); got != "" {
			t.Fatalf("prompt with the block: %.80q", got)
		}
	}
	// /clear: the next conversation reads the block before its first
	// prompt's hook.
	s.endConv("s-1")
	s.markedStart("s-1")
	if got := started(t, s, "clear"); got != modContextLine {
		t.Fatalf("clear: %q", got)
	}
	if !s.blockContext("s-1") {
		t.Fatal("the block was refused after /clear")
	}
	if got := prompted(t, s, true); got != "" {
		t.Fatalf("prompt after /clear: %.80q", got)
	}
	// A resume keeps what the conversation carries.
	s.newConvLocked("s-1")
	s.markedStart("s-1")
	if got := started(t, s, "resume"); got != modContextLine {
		t.Fatalf("resume: %q", got)
	}
	if got := prompted(t, s, true); got != "" {
		t.Fatalf("prompt after resume: %.80q", got)
	}
	// Without the mod UserPromptSubmit never brings it.
	if got := prompted(t, convServer(), false); got != "" {
		t.Fatalf("without the mod: %.80q", got)
	}
}

// TestContextSkipped: when another mod skipped the mod's hooks, the
// SessionStart hook gives the context itself, once, and the block is
// refused; after compaction, again.
func TestContextSkipped(t *testing.T) {
	s := convServer()
	full, _ := s.contextOf("s-1")()
	for _, src := range []string{"startup", "resume"} {
		s.newConvLocked("s-1")
		got := started(t, s, src)
		if !strings.HasPrefix(got, "terminatr: another Claude Code mod skipped terminatr's own") || !strings.HasSuffix(got, "in its place:\n\n"+string(full)) {
			t.Fatalf("%s, no mark: %.200q", src, got)
		}
		if s.blockContext("s-1") {
			t.Fatalf("%s: the block is added too", src)
		}
		if got := prompted(t, s, true); got != "" {
			t.Fatalf("%s: the prompt brings it again: %.80q", src, got)
		}
	}
	s.endConv("s-1")
	if got := started(t, s, "compact"); !strings.HasSuffix(got, string(full)) {
		t.Fatalf("compact, no mark: %.80q", got)
	}
	// Claude read the blocks before the SessionStart hook: the block
	// stands.
	s.endConv("s-1")
	s.blockContext("s-1")
	if got := started(t, s, "clear"); got != modContextLine {
		t.Fatalf("block first, then no mark: %q", got)
	}
}

// TestContextPromptSkipped: the mod's SessionStart ran but no block came
// by the second prompt: that prompt's hook gives the context, once.
func TestContextPromptSkipped(t *testing.T) {
	s := convServer()
	full, _ := s.contextOf("s-1")()
	s.markedStart("s-1")
	started(t, s, "startup")
	if got := prompted(t, s, true); got != "" {
		t.Fatalf("first prompt: %.80q", got)
	}
	if got := prompted(t, s, true); !strings.HasSuffix(got, string(full)) {
		t.Fatalf("second prompt: %.80q", got)
	}
	if got := prompted(t, s, true); got != "" {
		t.Fatalf("third prompt: %.80q", got)
	}
	if s.blockContext("s-1") {
		t.Fatal("the block is added too")
	}
	// No SessionStart reached the server: the first prompt gives it.
	s.newConvLocked("s-1")
	if got := prompted(t, s, true); !strings.HasSuffix(got, string(full)) {
		t.Fatalf("no SessionStart: %.80q", got)
	}
}

// TestContextFallbackLimit: a context past what Claude keeps of a hook's
// additionalContext is given as the commands that print it.
func TestContextFallbackLimit(t *testing.T) {
	s := &Server{log: log.New(os.Stderr, "", 0)}
	s.records = map[string]SessionRecord{
		"s-1": {ID: "s-1", Role: proto.RoleCoordinator, Project: "nowhere"},
	}
	full, _ := s.contextOf("s-1")()
	if len(full) <= hookContextLimit {
		t.Skipf("the coordinator's rules fit (%d bytes)", len(full))
	}
	got := started(t, s, "startup")
	if len(got) > 500 || !strings.Contains(got, "Run `tm skill coordinator` now") || !strings.Contains(got, "`tm context`") {
		t.Fatalf("coordinator: %q", got)
	}
	s.records["s-1"] = SessionRecord{ID: "s-1", Role: proto.RoleThread, Brief: "/p/b.md"}
	if got := string(s.fallbackContext("s-1", bytes.Repeat([]byte("x"), hookContextLimit))); !strings.Contains(got, "Run `tm skill thread` now") || !strings.Contains(got, "brief again: /p/b.md") {
		t.Fatalf("thread: %q", got)
	}
}

// TestContextSkipShown: a skip is recorded once the mod is heard from
// (a mod that never loaded is no skip), logged, and its project's
// coordinator is told once.
func TestContextSkipShown(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	s := &Server{log: log.New(&logged, "", 0)}
	s.records = map[string]SessionRecord{
		"s-1": {ID: "s-1", Role: proto.RoleThread, Project: "demo", Thread: "t-1"},
		"s-2": {ID: "s-2", Role: proto.RoleThread, Project: "demo", Thread: "t-2"},
	}
	start := func(id string) {
		t.Helper()
		if _, err := s.hookContextOf(id, true)(map[string]any{"hook_event_name": "SessionStart", "source": "startup"}); err != nil {
			t.Fatal(err)
		}
	}
	start("s-1")
	if items, _ := p.Inbox(); len(items) != 0 {
		t.Fatalf("told before the mod was heard: %v", items)
	}
	s.modHeard("s-1")
	s.modHeard("s-1")
	s.modHeard("s-2")
	start("s-2")
	items, _ := p.Inbox()
	if len(items) != 1 || items[0].Kind != "hooks-skipped" || items[0].Subject != "t-1" || !strings.Contains(items[0].Summary, "t-1 reported that terminatr's hooks were skipped by another mod") {
		t.Fatalf("inbox: %+v", items)
	}
	s.mu.Lock()
	recorded := s.convs["s-1"].recorded && s.convs["s-2"].recorded
	s.mu.Unlock()
	if !recorded {
		t.Fatal("a skip wasn't recorded")
	}
	if n := strings.Count(logged.String(), "hook didn't run"); n != 2 {
		t.Fatalf("logged %d skips:\n%s", n, logged.String())
	}
}

// TestContextSlowRender: a context that renders past what tm hook waits
// reached nobody, so it isn't counted as given: the block is still
// served, and without one the next prompt's hook gives it again.
func TestContextSlowRender(t *testing.T) {
	defer func(d time.Duration) { hookDelivered = d }(hookDelivered)
	hookDelivered = 10 * time.Millisecond
	s := convServer()
	full, _ := s.contextOf("s-1")()
	slow := func() ([]byte, error) {
		time.Sleep(30 * time.Millisecond)
		return full, nil
	}
	start := map[string]any{"hook_event_name": "SessionStart", "source": "startup"}
	if b, _ := s.startContext("s-1", start, slow); !bytes.HasSuffix(b, full) {
		t.Fatalf("slow SessionStart: %.80q", b)
	}
	if !s.blockContext("s-1") {
		t.Fatal("a late SessionStart refused the block")
	}
	// No block after all: the first prompt gives it (no SessionStart was
	// counted), a slow one again at the next prompt, and a quick one is
	// the last.
	s.endConv("s-1")
	if b, _ := s.promptContext("s-1", slow); !bytes.HasSuffix(b, full) {
		t.Fatalf("slow prompt: %.80q", b)
	}
	if b, _ := s.promptContext("s-1", s.contextOf("s-1")); !bytes.HasSuffix(b, full) {
		t.Fatalf("prompt after a slow one: %.80q", b)
	}
	if b, _ := s.promptContext("s-1", s.contextOf("s-1")); len(b) != 0 {
		t.Fatalf("prompt after a quick one: %.80q", b)
	}
	if s.blockContext("s-1") {
		t.Fatal("the block is added after a hook gave it")
	}
}

// TestContextSkipCleared: a session flagged as skipped loses the flag
// once a later conversation of it is marked by the mod again.
func TestContextSkipCleared(t *testing.T) {
	s := convServer()
	sess := &session.Session{}
	s.sessions = map[string]*session.Session{"s-1": sess}
	s.modHeard("s-1")
	started(t, s, "startup")
	if got := sess.HooksSkipped(); got != skippedBy {
		t.Fatalf("after a skip: %q", got)
	}
	s.endConv("s-1")
	s.markedStart("s-1")
	if got := started(t, s, "clear"); got != modContextLine {
		t.Fatalf("marked clear: %q", got)
	}
	if got := sess.HooksSkipped(); got != "" {
		t.Fatalf("after a marked start: %q", got)
	}
	// Skipped again: flagged again.
	s.endConv("s-1")
	started(t, s, "clear")
	if got := sess.HooksSkipped(); got != skippedBy {
		t.Fatalf("after a second skip: %q", got)
	}
}

// TestContextAfterExit: a hook racing the session's exit leaves no
// conversation state behind.
func TestContextAfterExit(t *testing.T) {
	s := convServer()
	s.markedStart("s-1")
	s.mu.Lock()
	delete(s.records, "s-1")
	delete(s.convs, "s-1")
	s.mu.Unlock()
	s.markedStart("s-1")
	s.modHeard("s-1")
	s.blockContext("s-1")
	s.endConv("s-1")
	started(t, s, "startup")
	prompted(t, s, true)
	s.mu.Lock()
	n := len(s.convs)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d conversation entries after the exit", n)
	}
}

// TestContextBlockDuringRender: a block served while a command hook
// rendered the context keeps the conversation's record as the block's.
func TestContextBlockDuringRender(t *testing.T) {
	s := convServer()
	full, _ := s.contextOf("s-1")()
	during := func() ([]byte, error) {
		if !s.blockContext("s-1") {
			t.Error("the block was refused while the hook rendered")
		}
		return full, nil
	}
	if b, _ := s.promptContext("s-1", during); !bytes.HasSuffix(b, full) {
		t.Fatalf("prompt: %.80q", b)
	}
	s.mu.Lock()
	via := s.convs["s-1"].via
	s.mu.Unlock()
	if via != viaBlock {
		t.Fatalf("via %q, want the block's", via)
	}
	if !s.blockContext("s-1") {
		t.Fatal("the block is refused after it was served")
	}
}
