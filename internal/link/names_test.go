package link

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/peer"
)

// TestPeerNames: a paired server's display name (tm peer name) and its
// alias name the same server in send, log and link settings; what is
// stored stays keyed by the alias, so a rename changes nothing stored,
// and what agents read says the name.
func TestPeerNames(t *testing.T) {
	sv := setupServers(t)
	human := caller.Caller{Kind: caller.Human}
	coordAcme := caller.Caller{Kind: caller.Coordinator, Project: "acme"}
	toMini := &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}
	name := func(dir, who, n string) {
		t.Helper()
		if _, err := peer.SetName(peer.DirIn(dir), who, n, nil); err != nil {
			t.Fatal(err)
		}
	}
	name(sv.studio, "mini", "work")
	name(sv.mini, "studio", "home")

	acme := sv.open(sv.studio, "acme")
	sv.in(sv.studio, func() {
		if err := Set(human, "acme", "work/web", "both", nil); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(sv.studio, "config.toml"))
		if !strings.Contains(string(b), `"mini/web" = "both"`) || strings.Contains(string(b), "work/") {
			t.Fatalf("config keyed by the name:\n%s", b)
		}
		for i, to := range []string{"work/api", "mini/api"} {
			m, err := Send(coordAcme, acme, to, Opts{Text: "hello"})
			if err != nil || m.Peer != "mini/api" {
				t.Fatalf("send %d to %s: %+v %v", i, to, m, err)
			}
		}
		if _, err := os.Stat(acme.Path("messages", "@mini", "api")); err != nil {
			t.Fatal("no messages/@mini/api")
		}
		for _, other := range []string{"work/api", "mini/api"} {
			if msgs, err := Log(acme, other); err != nil || len(msgs) != 2 {
				t.Fatalf("log %s: %d %v", other, len(msgs), err)
			}
		}
		for _, r := range Rows(mustCfg(t), acme) {
			if r.Peer == "mini/api" && r.Shown != "work/api" {
				t.Fatalf("row %+v", r)
			}
		}
		Flush(context.Background(), toMini)
	})
	api := sv.open(sv.mini, "api")
	sv.in(sv.mini, func() {
		its := items(t, api, KindMessage)
		if len(its) != 2 || its[0].Subject != "studio/acme" || !strings.HasPrefix(its[0].Summary, "home/acme wrote ") {
			t.Fatalf("items %+v", its)
		}
		got, _ := Read(api, its[0].Message)
		if f := Frame("api", got); !strings.Contains(f, "a project on the paired server home)") {
			t.Fatalf("frame:\n%s", f)
		}
	})

	// Renamed: the alias-keyed config and folders go on, by either word.
	name(sv.studio, "work", "office")
	sv.in(sv.studio, func() {
		if _, err := Send(coordAcme, acme, "office/api", Opts{Text: "again"}); err != nil {
			t.Fatal(err)
		}
		if _, err := Send(coordAcme, acme, "work/api", Opts{Text: "old name"}); code(err) == "" {
			t.Fatal("the old name still names it")
		}
		if msgs, _ := Log(acme, "office/api"); len(msgs) != 3 {
			t.Fatalf("log after the rename: %d", len(msgs))
		}
	})
	// Cleared: the alias shows again.
	name(sv.studio, "office", "")
	sv.in(sv.studio, func() {
		if Shown("mini/api") != "mini/api" || Canon("office/api") != "office/api" {
			t.Fatal("still named")
		}
	})
}
