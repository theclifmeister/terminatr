package link

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// shareWeb adds the project web on mini, shared with paired servers.
func shareWeb(t *testing.T, sv *servers) *project.Project {
	f := filepath.Join(sv.mini, "config.toml")
	b, _ := os.ReadFile(f)
	os.WriteFile(f, append(b, []byte("\n[projects.web]\nshare = true\n")...), 0o600)
	sv.in(sv.mini, func() {
		if _, err := project.New(project.Options{Slug: "web"}); err != nil {
			t.Fatal(err)
		}
	})
	return sv.open(sv.mini, "web")
}

func entryOf(t *testing.T, sv *servers, dir, slug, key string) (l config.Link, ok bool) {
	t.Helper()
	sv.in(dir, func() {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		mine, _ := cfg.Links(slug)
		l, ok = mine[key]
	})
	return l, ok
}

func row(sv *servers, dir string, p *project.Project, key string) (r Row) {
	sv.in(dir, func() {
		cfg, _ := config.Load()
		for _, x := range Rows(cfg, p) {
			if x.Peer == key {
				r = x
			}
		}
	})
	return r
}

// TestRequestRoundTrip: acme on studio asks web on mini to link back.
// The request sets acme's own entry, waits in the outbox, lands in web's
// inbox for the user, who accepts it: only mini's config changes there,
// and the answer reaches acme's row. One request waits per pair; agents
// can neither ask nor answer.
func TestRequestRoundTrip(t *testing.T) {
	sv := setupServers(t)
	web := shareWeb(t, sv)
	acme := sv.open(sv.studio, "acme")
	human := caller.Caller{Kind: caller.Human}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "acme"}
	toMini := &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}
	toStudio := &wire{sv: sv, to: sv.studio, alias: "mini", id: sv.mid, up: true}

	var r *Request
	sv.in(sv.studio, func() {
		if _, err := Ask(coord, acme, "mini/web", "both"); err == nil || !strings.Contains(err.Error(), "user's") {
			t.Fatalf("a coordinator asked: %v", err)
		}
		if _, err := Ask(human, acme, "beta", "both"); err == nil {
			t.Fatal("a request to a project here")
		}
		var err error
		if r, err = Ask(human, acme, "mini/web", "send"); err != nil {
			t.Fatal(err)
		}
		if _, err := Ask(human, acme, "mini/web", "send"); err == nil || !strings.Contains(err.Error(), "waits for an answer") {
			t.Fatalf("a second request: %v", err)
		}
	})
	if !ValidRequestID(r.ID) || r.State != ReqQueued || !strings.Contains(r.ID, sv.sid.Short()[:4]) {
		t.Fatalf("request %+v", r)
	}
	if l, ok := entryOf(t, sv, sv.studio, "acme", "mini/web"); !ok || l.Allow != "send" || !l.Ask {
		t.Fatalf("acme's own entry: %+v %v", l, ok)
	}

	sv.in(sv.studio, func() { Flush(context.Background(), toMini) })
	its := items(t, web, KindRequest)
	if len(its) != 1 || !its[0].NeedsUser || its[0].Subject != "studio/acme" || its[0].Message != r.ID ||
		!strings.Contains(its[0].Summary, "send only on its side, so receive only here") {
		t.Fatalf("web's item: %+v", its)
	}
	if got := row(sv, sv.studio, acme, "mini/web"); got.Asked == nil || got.Asked.State != ReqWaiting || !strings.Contains(got.Words(), "asked to link back") {
		t.Fatalf("acme's row: %+v", got)
	}
	wr := row(sv, sv.mini, web, "studio/acme")
	if wr.Asks == nil || wr.Asks.ID != r.ID || !strings.Contains(wr.Words(), "asks to link") {
		t.Fatalf("web's row: %+v", wr)
	}
	if _, ok := entryOf(t, sv, sv.mini, "web", "studio/acme"); ok {
		t.Fatal("the request wrote mini's config")
	}

	sv.in(sv.mini, func() {
		if _, err := Answer(caller.Caller{Kind: caller.Coordinator, Project: "web"}, web, r.ID, true, "receive", false); err == nil {
			t.Fatal("a coordinator accepted")
		}
		got, err := Answer(human, web, r.ID, true, "receive", false)
		if err != nil || got.State != ReqAccepted {
			t.Fatalf("accept: %+v %v", got, err)
		}
		if _, err := Answer(human, web, r.ID, false, "", false); err == nil {
			t.Fatal("answered twice")
		}
	})
	if l, ok := entryOf(t, sv, sv.mini, "web", "studio/acme"); !ok || l.Allow != "receive" || l.Ask {
		t.Fatalf("web's own entry: %+v %v", l, ok)
	}
	if len(items(t, web, KindRequest)) != 0 {
		t.Fatal("the item stays after the answer")
	}

	sv.in(sv.mini, func() { Flush(context.Background(), toStudio) })
	got := row(sv, sv.studio, acme, "mini/web")
	if got.Asked == nil || got.Asked.State != ReqAccepted || got.Asked.Answer != "receive" || got.Asked.Words() != "accepted: receive only" {
		t.Fatalf("acme's row after the answer: %+v", got.Asked)
	}
	if its := items(t, acme, KindAnswer); len(its) != 1 || !strings.Contains(its[0].Summary, "accepted") {
		t.Fatalf("acme's answer item: %+v", its)
	}
	// Now the link carries acme's messages (the user's: acme's entry
	// asks first for its coordinator's).
	sv.in(sv.studio, func() {
		if _, err := Send(human, acme, "mini/web", Opts{Text: "hello"}); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toMini)
	})
	if len(items(t, web, KindMessage)) != 1 {
		t.Fatal("the message didn't arrive")
	}
	for _, j := range []struct {
		p    *project.Project
		want string
	}{{acme, "link.request mini/web " + r.ID}, {acme, "link.accepted mini/web"}, {web, "link.requested studio/acme"}, {web, "link.accept studio/acme " + r.ID + " (receive)"}} {
		b, _ := os.ReadFile(j.p.Path("JOURNAL.md"))
		if !strings.Contains(string(b), j.want) {
			t.Errorf("%s's journal lacks %q:\n%s", j.p.Slug, j.want, b)
		}
	}
}

// TestRequestDeclinedAndRefused: a declined request changes nothing
// there; one to a project that isn't shared, or to a server that doesn't
// know the kind, is undelivered in fixed words.
func TestRequestDeclinedAndRefused(t *testing.T) {
	sv := setupServers(t)
	web := shareWeb(t, sv)
	acme := sv.open(sv.studio, "acme")
	human := caller.Caller{Kind: caller.Human}
	toMini := &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}
	toStudio := &wire{sv: sv, to: sv.studio, alias: "mini", id: sv.mid, up: true}

	var r *Request
	sv.in(sv.studio, func() {
		r, _ = Ask(human, acme, "mini/web", "both")
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		if _, err := Answer(human, web, r.ID, false, "", false); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toStudio)
	})
	if _, ok := entryOf(t, sv, sv.mini, "web", "studio/acme"); ok {
		t.Fatal("declining wrote an entry")
	}
	if got := row(sv, sv.studio, acme, "mini/web"); got.Asked == nil || got.Asked.State != ReqDeclined {
		t.Fatalf("declined: %+v", got.Asked)
	}

	// api isn't shared; nosuch isn't there: the same answer.
	for _, key := range []string{"mini/api", "mini/nosuch"} {
		sv.in(sv.studio, func() {
			q, err := Ask(human, acme, key, "both")
			if err != nil {
				t.Fatal(err)
			}
			Flush(context.Background(), toMini)
			for _, x := range Requests(acme) {
				if x.ID == q.ID && (x.State != ReqUndelivered || x.Code != "not-shared") {
					t.Fatalf("%s: %+v", key, x)
				}
			}
		})
	}
	if its := items(t, acme, KindUndelivered); len(its) != 2 || !strings.Contains(its[0].Summary, "isn't shared with this server") {
		t.Fatalf("undelivered items: %+v", its)
	}

	// A server before link requests answers bad-message, as for any
	// kind it doesn't know.
	old := &oldServer{}
	sv.in(sv.studio, func() {
		if _, err := Ask(human, acme, "mini/other", "both"); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), old)
	})
	its := items(t, acme, KindUndelivered)
	if len(its) != 3 || !strings.Contains(its[0].Summary+its[1].Summary+its[2].Summary, "a tm before 0.28.0 doesn't take link requests") {
		t.Fatalf("an older server: %+v", its)
	}
}

// oldServer answers as a server before link requests: bad-message for a
// kind it doesn't know.
type oldServer struct{}

func (oldServer) Ready(string) bool { return true }
func (oldServer) Call(_ context.Context, _, _ string, params, result any) error {
	res := proto.PeerMessageResult{Status: proto.PeerDelivered}
	if k := params.(proto.PeerMessage).Kind; k != proto.PeerMessageText && k != proto.PeerMessageRead {
		res = proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	*result.(*proto.PeerMessageResult) = res
	return nil
}

// TestRequestRateAndExpiry: at most RequestsPerHour go to one server in
// an hour, and take from one; a request not answered in RequestExpiry
// expires on both sides, its item done.
func TestRequestRateAndExpiry(t *testing.T) {
	sv := setupServers(t)
	web := shareWeb(t, sv)
	acme := sv.open(sv.studio, "acme")
	human := caller.Caller{Kind: caller.Human}
	toMini := &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}

	sv.in(sv.studio, func() {
		for i := range RequestsPerHour {
			if _, err := Ask(human, acme, "mini/p"+string(rune('a'+i)), "both"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Ask(human, acme, "mini/web", "both"); err == nil || !strings.Contains(err.Error(), "an hour") {
			t.Fatalf("over the rate: %v", err)
		}
		Flush(context.Background(), toMini)
		*sv.clock = sv.clock.Add(time.Hour)
		if _, err := Ask(human, acme, "mini/web", "both"); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toMini)
	})
	if len(items(t, web, KindRequest)) != 1 {
		t.Fatal("no request at web")
	}

	// The receiver's own rate: a server that sends more than it should.
	sv.in(sv.mini, func() {
		var last proto.PeerMessageResult
		for i := range RequestsPerHour + 1 {
			last = Receive("studio", sv.sid, proto.PeerMessage{Epoch: "x", Seq: i + 1, Kind: proto.PeerLinkRequest, From: "acme", To: "web",
				ID: "R-" + sv.sid.Short()[:4] + "-" + string(rune('1'+i)) + "00", By: "human", Allow: "both"})
		}
		if last.Status != proto.PeerRefused || last.Code != "rate-limited" {
			t.Fatalf("the receiver's rate: %+v", last)
		}
		// A request from another server's id is refused.
		if res := Receive("studio", sv.mid, proto.PeerMessage{Epoch: "y", Seq: 1, Kind: proto.PeerLinkRequest, From: "acme", To: "web",
			ID: "R-" + sv.sid.Short()[:4] + "-999", By: "human", Allow: "both"}); res.Code != "bad-message" {
			t.Fatalf("a foreign id: %+v", res)
		}
	})

	*sv.clock = sv.clock.Add(RequestExpiry + time.Minute)
	sv.in(sv.mini, func() { ExpireRequests() })
	sv.in(sv.studio, func() { ExpireRequests() })
	if len(items(t, web, KindRequest)) != 0 {
		t.Fatal("an expired request's item stays")
	}
	if got := row(sv, sv.studio, acme, "mini/web"); got.Asked == nil || got.Asked.State != ReqExpired {
		t.Fatalf("expired: %+v", got.Asked)
	}
}
