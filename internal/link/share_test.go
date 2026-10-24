package link

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// shareOf is the share request id as the home dir keeps it, out or in.
func shareOf(sv *servers, dir, id string, out bool) (r proto.ShareRequest) {
	sv.in(dir, func() {
		for _, x := range ShareRequests() {
			if x.ID == id && x.Out == out {
				r = x
			}
		}
	})
	return r
}

// shareWaits counts the incoming share requests that wait.
func shareWaits() int {
	n := 0
	byAlias, _ := ShareState()
	for _, rs := range byAlias {
		for _, r := range rs {
			if !r.Out && r.Pending() {
				n++
			}
		}
	}
	return n
}

// noItems fails when a share request reached p's inbox: they are the
// server's, never a project's.
func noItems(t *testing.T, p *project.Project) {
	t.Helper()
	if its, _ := p.Inbox(); len(its) != 0 {
		t.Fatalf("%s's inbox: %+v", p.Slug, its)
	}
}

// TestShareRoundTrip: studio's user asks mini's to share a project, for
// acme, with a note. The request waits in the outbox, then on mini for
// its user, who shares api: mini's config says share = true, and the
// answer reaches studio, in its words and no inbox. One waits per
// server; agents can neither ask nor answer.
func TestShareRoundTrip(t *testing.T) {
	sv := setupServers(t)
	acme := sv.open(sv.studio, "acme")
	api := sv.open(sv.mini, "api")
	human := caller.Caller{Kind: caller.Human}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "acme"}
	toMini := &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}
	toStudio := &wire{sv: sv, to: sv.studio, alias: "mini", id: sv.mid, up: true}

	var r *proto.ShareRequest
	sv.in(sv.studio, func() {
		if _, err := AskShare(coord, "mini", "acme", "", false); err == nil || !strings.Contains(err.Error(), "user's") {
			t.Fatalf("a coordinator asked: %v", err)
		}
		if _, err := AskShare(human, "nosuch", "", "", false); err == nil || !strings.Contains(err.Error(), "no paired server") {
			t.Fatalf("a server not paired: %v", err)
		}
		if _, err := AskShare(human, "mini", "nosuch", "", false); err == nil {
			t.Fatal("for a project that isn't here")
		}
		if _, err := AskShare(human, "mini", "", strings.Repeat("x", ShareNoteMax+1), false); err == nil || !strings.Contains(err.Error(), "120 bytes") {
			t.Fatalf("a long note: %v", err)
		}
		if _, err := AskShare(human, "mini", "", "", true); err == nil || !strings.Contains(err.Error(), "runs a tm before "+ShareSince) {
			t.Fatalf("an older server: %v", err)
		}
		var err error
		if r, err = AskShare(human, "mini", "acme", "the deploy\nscripts\x1b[31m", false); err != nil {
			t.Fatal(err)
		}
		if _, err := AskShare(human, "mini", "", "", false); err == nil || !strings.Contains(err.Error(), "asked already ("+r.ID) {
			t.Fatalf("a second request: %v", err)
		}
	})
	if !ValidShareID(r.ID) || r.State != proto.ShareQueued || !strings.Contains(r.ID, sv.sid.Short()[:4]) || r.Note != "the deploy scripts[31m" {
		t.Fatalf("request %+v", r)
	}

	sv.in(sv.studio, func() { Flush(context.Background(), toMini) })
	if got := shareOf(sv, sv.studio, r.ID, true); got.State != proto.ShareWaiting || !strings.Contains(got.Summary, "waiting on mini's user") {
		t.Fatalf("studio's request: %+v", got)
	}
	in := shareOf(sv, sv.mini, r.ID, false)
	want := `studio asks you to share a project with it, for its acme: "the deploy scripts[31m"; answer in Network (N)`
	if in.State != proto.ShareWaiting || in.Summary != want || in.Peer != "studio" {
		t.Fatalf("mini's request: %+v", in)
	}
	sv.in(sv.mini, func() {
		if n := shareWaits(); n != 1 {
			t.Fatalf("ShareWaits %d", n)
		}
		if _, err := AnswerShare(caller.Caller{Kind: caller.Coordinator, Project: "api"}, r.ID, true, "api"); err == nil {
			t.Fatal("a coordinator accepted")
		}
		if _, err := AnswerShare(human, "S-0000-1", false, ""); err == nil || !strings.Contains(err.Error(), "no share request") {
			t.Fatalf("an unknown request: %v", err)
		}
		if _, err := AnswerShare(human, r.ID, true, "nosuch"); err == nil {
			t.Fatal("accepted with a project that isn't there")
		}
		got, err := AnswerShare(human, r.ID, true, "api")
		if err != nil || got.State != proto.ShareIsAccepted || got.Project != "api" {
			t.Fatalf("accept: %+v %v", got, err)
		}
		if _, err := AnswerShare(human, r.ID, false, ""); err == nil {
			t.Fatal("answered twice")
		}
		cfg, _ := config.Load()
		if s, err := cfg.Safety("api"); err != nil || !s.Share {
			t.Fatalf("api isn't shared: %+v %v", s, err)
		}
		if n := shareWaits(); n != 0 {
			t.Fatalf("ShareWaits after the answer %d", n)
		}
		Flush(context.Background(), toStudio)
	})
	if got := shareOf(sv, sv.studio, r.ID, true); got.State != proto.ShareIsAccepted {
		t.Fatalf("studio after the answer: %+v", got)
	}
	if got := shareOf(sv, sv.studio, r.ID, true); !strings.HasPrefix(got.Summary, "mini accepted your share request "+r.ID) {
		t.Fatalf("studio's words: %q", got.Summary)
	}
	noItems(t, acme)
	noItems(t, api)
	sv.in(sv.studio, func() {
		_, rev := ShareState()
		if rev == 0 {
			t.Fatal("no changes counted")
		}
	})
	for _, j := range []struct {
		file, want string
	}{{acme.Path("JOURNAL.md"), "peer.share-request mini " + r.ID + " (for acme)"}, {acme.Path("JOURNAL.md"), "peer.share-accepted mini " + r.ID},
		{api.Path("JOURNAL.md"), "peer.share-accepted studio " + r.ID + " (api)"}, {api.Path("JOURNAL.md"), "settings.share"}} {
		b, _ := os.ReadFile(j.file)
		if !strings.Contains(string(b), j.want) {
			t.Errorf("journal lacks %q:\n%s", j.want, b)
		}
		if strings.Contains(string(b), "deploy") {
			t.Errorf("the note in a journal:\n%s", b)
		}
	}
}

// TestShareDeclined: a declined request shares nothing, and the asker
// hears of it; no inbox does.
func TestShareDeclined(t *testing.T) {
	sv := setupServers(t)
	acme := sv.open(sv.studio, "acme")
	human := caller.Caller{Kind: caller.Human}
	toMini := &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}
	toStudio := &wire{sv: sv, to: sv.studio, alias: "mini", id: sv.mid, up: true}

	var r *proto.ShareRequest
	sv.in(sv.studio, func() {
		r, _ = AskShare(human, "mini", "", "", false)
		Flush(context.Background(), toMini)
	})
	if got := shareOf(sv, sv.mini, r.ID, false); got.Summary != "studio asks you to share a project with it; answer in Network (N)" {
		t.Fatalf("no for, no note: %q", got.Summary)
	}
	sv.in(sv.mini, func() {
		if _, err := AnswerShare(human, r.ID, false, ""); err != nil {
			t.Fatal(err)
		}
		cfg, _ := config.Load()
		if s, _ := cfg.Safety("api"); s.Share {
			t.Fatal("declining shared")
		}
		Flush(context.Background(), toStudio)
	})
	if got := shareOf(sv, sv.studio, r.ID, true); got.State != proto.ShareIsDeclined || !strings.Contains(got.Summary, "declined") {
		t.Fatalf("declined: %+v", got)
	}
	noItems(t, acme)
	// Answered, a new one may go.
	sv.in(sv.studio, func() {
		if _, err := AskShare(human, "mini", "", "", false); err != nil {
			t.Fatal(err)
		}
	})
}

// TestShareOlderServer: a server before share requests answers
// bad-message and takes the seq: the request is undelivered in fixed
// words (no inbox hears of it), and the message behind it still goes.
func TestShareOlderServer(t *testing.T) {
	sv := setupServers(t)
	acme := sv.open(sv.studio, "acme")
	human := caller.Caller{Kind: caller.Human}
	old := &oldServer{}
	var r *proto.ShareRequest
	sv.in(sv.studio, func() {
		r, _ = AskShare(human, "mini", "acme", "", false)
		if _, err := Send(human, acme, "mini/api", Opts{Text: "after it"}); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), old)
	})
	got := shareOf(sv, sv.studio, r.ID, true)
	want := "mini runs a tm before " + ShareSince + ", which doesn't take share requests; ask its user yourself"
	if got.State != proto.ShareUndelivered || got.Code != "bad-message" || !strings.HasSuffix(got.Summary, want) {
		t.Fatalf("undelivered: %+v", got)
	}
	if got.Summary != "your share request "+r.ID+" to mini wasn't delivered: "+want {
		t.Fatalf("the words: %q", got.Summary)
	}
	noItems(t, acme)
	var sent bool
	sv.in(sv.studio, func() {
		msgs, _ := Log(acme, "mini/api")
		sent = len(msgs) == 1 && msgs[0].State == StateSent
	})
	if !sent {
		t.Fatal("the message behind it didn't go")
	}
}

// TestShareRateExpiryUndelivered: at most RequestsPerHour go to one
// server and come from one; the receiver checks the id's fingerprint; a
// request not taken in Expiry is undelivered, and one unanswered for
// RequestExpiry expires on both sides.
func TestShareRateExpiryUndelivered(t *testing.T) {
	sv := setupServers(t)
	acme := sv.open(sv.studio, "acme")
	human := caller.Caller{Kind: caller.Human}
	toMini := &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}
	toStudio := &wire{sv: sv, to: sv.studio, alias: "mini", id: sv.mid, up: true}

	// Asked and declined RequestsPerHour times, the next is over.
	sv.in(sv.studio, func() {
		for range RequestsPerHour {
			r, err := AskShare(human, "mini", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			Flush(context.Background(), toMini)
			sv.in(sv.mini, func() {
				AnswerShare(human, r.ID, false, "")
				Flush(context.Background(), toStudio)
			})
		}
		if _, err := AskShare(human, "mini", "", "", false); err == nil || !strings.Contains(err.Error(), "an hour") {
			t.Fatalf("over the rate: %v", err)
		}
	})

	// The receiver's own rate, and the fingerprint.
	sv.in(sv.mini, func() {
		fp := sv.sid.Short()[:4]
		if res := Receive("studio", sv.sid, proto.PeerMessage{Epoch: "x", Seq: 1, Kind: proto.PeerShareRequest, ID: "S-" + fp + "-900", By: "human"}); res.Code != "rate-limited" {
			t.Fatalf("the receiver's rate: %+v", res)
		}
		if res := Receive("studio", sv.mid, proto.PeerMessage{Epoch: "y", Seq: 1, Kind: proto.PeerShareRequest, ID: "S-" + fp + "-901", By: "human"}); res.Code != "bad-message" {
			t.Fatalf("a foreign id: %+v", res)
		}
		for i, m := range []proto.PeerMessage{
			{Kind: proto.PeerShareRequest, ID: "S-" + fp + "-902", By: "coordinator"},
			{Kind: proto.PeerShareRequest, ID: "S-" + fp + "-903", By: "human", For: "Bad Slug"},
			{Kind: proto.PeerShareRequest, ID: "S-" + fp + "-904", By: "human", Text: strings.Repeat("x", ShareNoteMax+1)},
			{Kind: proto.PeerShareRequest, ID: "S-" + fp + "-905", By: "human", From: "acme", To: "api"},
			{Kind: proto.PeerShareAnswer, Re: "S-" + fp + "-1", Allow: "maybe"},
		} {
			m.Epoch, m.Seq = "z", i+1
			if res := Receive("studio", sv.sid, m); res.Code != "bad-message" {
				t.Errorf("%+v: %+v", m, res)
			}
		}
	})

	// An hour on: one not taken in 72 hours is undelivered.
	*sv.clock = sv.clock.Add(time.Hour)
	var r *proto.ShareRequest
	sv.in(sv.studio, func() {
		var err error
		if r, err = AskShare(human, "mini", "acme", "", false); err != nil {
			t.Fatal(err)
		}
		*sv.clock = sv.clock.Add(Expiry + time.Minute)
		Flush(context.Background(), &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid})
	})
	if got := shareOf(sv, sv.studio, r.ID, true); got.State != proto.ShareUndelivered || got.Code != "expired" {
		t.Fatalf("not taken: %+v", got)
	}
	if got := shareOf(sv, sv.studio, r.ID, true); !strings.HasSuffix(got.Summary, "mini didn't take it within 72 hours") {
		t.Fatalf("the undelivered words: %q", got.Summary)
	}

	// Taken, then unanswered for 7 days: expired on both sides.
	sv.in(sv.studio, func() {
		if r, _ = AskShare(human, "mini", "acme", "", false); r == nil {
			t.Fatal("no request")
		}
		Flush(context.Background(), toMini)
	})
	*sv.clock = sv.clock.Add(RequestExpiry + time.Minute)
	sv.in(sv.mini, ExpireShares)
	sv.in(sv.studio, ExpireShares)
	if got := shareOf(sv, sv.mini, r.ID, false); got.State != proto.ShareExpired {
		t.Fatalf("mini's: %+v", got)
	}
	if got := shareOf(sv, sv.studio, r.ID, true); got.State != proto.ShareExpired {
		t.Fatalf("studio's: %+v", got)
	}
	sv.in(sv.mini, func() {
		if n := shareWaits(); n != 0 {
			t.Fatalf("an expired request waits: %d", n)
		}
	})
	if got := shareOf(sv, sv.studio, r.ID, true); !strings.Contains(got.Summary, "expired unanswered") {
		t.Fatalf("the expired words: %q", got.Summary)
	}
	noItems(t, acme)
}
