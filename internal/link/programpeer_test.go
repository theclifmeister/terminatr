package link

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// programWire is a wire whose other server's hello lists "program" when
// cap says so, and that keeps each answer.
type programWire struct {
	*wire
	cap  bool
	got  []proto.PeerMessageResult
	kind []string
}

func (w *programWire) ProgramReady(string) bool { return w.up && w.cap }

func (w *programWire) Call(ctx context.Context, alias, method string, params, result any) error {
	err := w.wire.Call(ctx, alias, method, params, result)
	w.got = append(w.got, *result.(*proto.PeerMessageResult))
	w.kind = append(w.kind, params.(proto.PeerMessage).Kind)
	return err
}

func setPrograms(t *testing.T, on bool) {
	t.Helper()
	old := ProgramsOn()
	SetPrograms(on)
	t.Cleanup(func() { SetPrograms(old) })
}

// queueProgram puts a program kind's entry in the outbox to mini, as
// the program kinds (a later build step) will.
func queueProgram(t *testing.T) proto.PeerMessage {
	t.Helper()
	m := proto.PeerMessage{Kind: proto.PeerProgramPrefix + "test", From: "acme", To: "api", ID: "M-0000-1"}
	if err := withStore(func(s *store) error { return s.queue("mini", m) }); err != nil {
		t.Fatal(err)
	}
	return m
}

func programEntries(t *testing.T) int {
	t.Helper()
	root, _ := outboxDir()
	files, _ := os.ReadDir(filepath.Join(root, "mini", programStream))
	return len(files)
}

// TestProgramOutboxPaused: with programs off, the program stream to a
// paired server waits, whatever its age, while a coordinator's message
// goes; back on, what waited past Expiry is settled as expired, and a
// fresh one goes, only to a server whose hello lists "program".
func TestProgramOutboxPaused(t *testing.T) {
	sv := setupServers(t)
	coordAcme := caller.Caller{Kind: caller.Coordinator, Project: "acme"}
	acme := sv.open(sv.studio, "acme")
	w := &programWire{wire: &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}, cap: true}
	setPrograms(t, false)
	sv.in(sv.studio, func() {
		queueProgram(t)
		m, err := Send(coordAcme, acme, "mini/api", Opts{Text: "hello"})
		if err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), w)
		if got, _ := Read(acme, m.ID); got.State != StateSent || w.calls != 1 {
			t.Fatalf("the message beside a paused program stream: %s, %d calls", got.State, w.calls)
		}
		if n := programEntries(t); n != 1 {
			t.Fatalf("paused program stream: %d entries", n)
		}
		if b := Backlogs()["mini"]; b.Queued != 1 {
			t.Fatalf("backlog counts the program stream: %+v", b)
		}
		*sv.clock = sv.clock.Add(Expiry + time.Hour)
		Flush(context.Background(), w)
		if n := programEntries(t); n != 1 || w.calls != 1 {
			t.Fatalf("paused past expiry: %d entries, %d calls", n, w.calls)
		}

		SetPrograms(true)
		Flush(context.Background(), w)
		if n := programEntries(t); n != 0 || w.calls != 1 {
			t.Fatalf("back on, expired: %d entries, %d calls", n, w.calls)
		}

		queueProgram(t)
		w.cap = false
		Flush(context.Background(), w)
		if n := programEntries(t); n != 1 || w.calls != 1 {
			t.Fatalf("to a server without the cap: %d entries, %d calls", n, w.calls)
		}
		w.cap = true
		Flush(context.Background(), w)
		if n := programEntries(t); n != 0 || w.calls != 2 {
			t.Fatalf("to a server with the cap: %d entries, %d calls", n, w.calls)
		}
	})
	// Its own sequence: mini took coordinator message 1 and still takes
	// program message 1 (it knows no "program-test": bad-message, not a
	// duplicate).
	if last := w.got[len(w.got)-1]; last.Status != proto.PeerRefused || last.Code != "bad-message" {
		t.Fatalf("program 1 after message 1: %+v", last)
	}
}

// TestProgramReceiveOff: a server with programs off refuses every
// program kind programs-off, in the words the sender's item shows.
func TestProgramReceiveOff(t *testing.T) {
	sv := setupServers(t)
	setPrograms(t, false)
	var res proto.PeerMessageResult
	sv.in(sv.mini, func() {
		res = Receive("studio", sv.sid, proto.PeerMessage{Kind: proto.PeerProgramPrefix + "invite", Epoch: "e", Seq: 1, From: "acme", To: "api", ID: "M-0000-1"})
	})
	if res.Status != proto.PeerRefused || res.Code != config.ProgramsOff {
		t.Fatalf("programs off: %+v", res)
	}
	if got := replaceWords(RefusalWords[res.Code], "mini/api", "acme"); got != "mini/api's server has programs off" {
		t.Fatalf("words: %q", got)
	}
	// A coordinator's message isn't one.
	sv.in(sv.mini, func() {
		res = Receive("studio", sv.sid, proto.PeerMessage{Kind: proto.PeerMessageRead, Epoch: "e", Seq: 1, From: "acme", To: "api", ID: "M-0000-1"})
	})
	if res.Code == config.ProgramsOff {
		t.Fatalf("a read receipt with programs off: %+v", res)
	}
}
