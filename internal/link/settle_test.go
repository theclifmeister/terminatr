package link

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
)

// TestSettleLocal: two files offered under ask; turning the link's files
// to auto takes the one within b's link_file_mb as if it had just
// arrived, closing its ask, and leaves the one over it waiting; turning
// them off then declines that one.
func TestSettleLocal(t *testing.T) {
	a, b, _ := setup(t)
	ca := coord
	ca.Project = "a"
	libFile(t, a, "t-0001", "small.bin", 100<<10)
	libFile(t, a, "t-0001", "big.bin", 2<<20)
	small, err := Send(ca, a, "b", Opts{Text: "small", File: "t-0001/small.bin"})
	if err != nil {
		t.Fatal(err)
	}
	big, err := Send(ca, a, "b", Opts{Text: "big", File: "t-0001/big.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(items(t, b, KindFile)); n != 2 {
		t.Fatalf("%d asks", n)
	}
	cfg := mustConfig(t)
	os.WriteFile(cfg.Path, []byte("[projects.a.links]\nb = \"both\"\n\n[projects.b]\nlink_file_mb = 1\n\n[projects.b.links]\na = \"both\"\n"), 0o600)
	if fit, over := CountOffers(Transfers(b, "a"), 1); fit != 1 || over != 1 {
		t.Fatalf("count %d %d", fit, over)
	}

	taken, declined, err := SetFiles(human, "a", "b", config.FilesAuto)
	if err != nil || taken != 1 || declined != 0 {
		t.Fatalf("auto: took %d, declined %d, %v", taken, declined, err)
	}
	if in := transfer(t, b, small.Transfer); in.State != FileReceived || !in.Auto {
		t.Fatalf("small %+v", in)
	}
	if out := transfer(t, a, small.Transfer); out.State != FileReceived {
		t.Fatalf("small, the sender's %+v", out)
	}
	if in := transfer(t, b, big.Transfer); in.State != FileOffered {
		t.Fatalf("big %+v", in)
	}
	its := items(t, b, KindFile)
	asks := 0
	for _, it := range its {
		if it.NeedsUser {
			asks++
			if it.Message != big.Transfer {
				t.Errorf("the ask left open is %s's", it.Message)
			}
		} else if it.Message != small.Transfer || !strings.Contains(it.Summary, "was received") {
			t.Errorf("item %+v", it)
		}
	}
	if asks != 1 || len(its) != 2 {
		t.Fatalf("items %+v", its)
	}
	if !strings.Contains(journal(b), "link.file-accept a "+small.Transfer+" (auto)") {
		t.Errorf("journal:\n%s", journal(b))
	}

	taken, declined, err = SetFiles(human, "b", "a", config.FilesOff)
	if err != nil || taken != 0 || declined != 1 {
		t.Fatalf("off: took %d, declined %d, %v", taken, declined, err)
	}
	if in, out := transfer(t, b, big.Transfer), transfer(t, a, big.Transfer); in.State != FileDeclined || out.State != FileDeclined {
		t.Fatalf("big %+v, the sender's %+v", in, out)
	}
	for _, it := range items(t, b, KindFile) {
		if it.NeedsUser {
			t.Errorf("an ask is still open: %+v", it)
		}
	}
	if its := items(t, a, KindFile); len(its) != 1 || !strings.Contains(its[0].Summary, "b declined your file big.bin") {
		t.Errorf("a's items %+v", its)
	}
}

// TestSettleRemote: an offer from a paired server waits under ask; a
// hand edit of mini's config to auto is taken up by the flusher, which
// accepts it, closes the ask and answers studio; the file then comes as
// an auto one does. Another, with the link's files turned off, is
// declined and studio hears of it.
func TestSettleRemote(t *testing.T) {
	sv := setupServers(t)
	m, _, acme, api, toMini, toStudio := remoteFile(t, sv)
	sv.in(sv.mini, func() {
		if n := len(items(t, api, KindFile)); n != 1 {
			t.Fatalf("%d asks", n)
		}
		os.WriteFile(filepath.Join(sv.mini, "config.toml"), []byte("[projects.api.links]\n\"studio/acme\" = { allow = \"both\", files = \"auto\" }\n"), 0o600)
		Flush(context.Background(), toStudio)
		if in := transfer(t, api, m.Transfer); in.State != FileAccepted || !in.Auto {
			t.Fatalf("in %+v", in)
		}
		if n := len(items(t, api, KindFile)); n != 0 {
			t.Fatalf("%d items left", n)
		}
		fetch(context.Background(), &pipe{sv: sv}, transfer(t, api, m.Transfer))
		its := items(t, api, KindFile)
		if in := transfer(t, api, m.Transfer); in.State != FileReceived || len(its) != 1 || its[0].NeedsUser {
			t.Fatalf("in %+v, items %+v", in, its)
		}
		Flush(context.Background(), toStudio)
		os.WriteFile(filepath.Join(sv.mini, "config.toml"), []byte("[projects.api.links]\n\"studio/acme\" = \"both\"\n"), 0o600)
	})
	var m2 *Message
	sv.in(sv.studio, func() {
		if out := transfer(t, acme, m.Transfer); out.State != FileReceived {
			t.Fatalf("out %+v", out)
		}
		var err error
		if m2, err = Send(caller.Caller{Kind: caller.Coordinator, Project: "acme"}, acme, "mini/api", Opts{Text: "again", File: "t-0001/load test.csv"}); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		if in := transfer(t, api, m2.Transfer); in.State != FileOffered {
			t.Fatalf("in %+v", in)
		}
		taken, declined, err := SetFiles(human, "api", "studio/acme", config.FilesOff)
		if err != nil || taken != 0 || declined != 1 {
			t.Fatalf("off: took %d, declined %d, %v", taken, declined, err)
		}
		if in := transfer(t, api, m2.Transfer); in.State != FileDeclined {
			t.Fatalf("in %+v", in)
		}
		for _, it := range items(t, api, KindFile) {
			if it.NeedsUser {
				t.Errorf("an ask is still open: %+v", it)
			}
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		its := items(t, acme, KindFile)
		if out := transfer(t, acme, m2.Transfer); out.State != FileDeclined || len(its) != 1 || !strings.Contains(its[0].Summary, "declined your file") {
			t.Fatalf("out %+v, items %+v", out, its)
		}
	})
}

// TestSave: the link dialog's one act writes this side's direction, ask
// and files and the other side's mirror, keeping its own ask, then
// settles the offers waiting: auto takes the one within the size, off
// (unlinking) declines the rest and removes both entries.
func TestSave(t *testing.T) {
	a, b, _ := setup(t)
	ca := coord
	ca.Project = "a"
	libFile(t, a, "t-0001", "small.bin", 100<<10)
	libFile(t, a, "t-0001", "big.bin", 2<<20)
	small, err := Send(ca, a, "b", Opts{Text: "small", File: "t-0001/small.bin"})
	if err != nil {
		t.Fatal(err)
	}
	big, err := Send(ca, a, "b", Opts{Text: "big", File: "t-0001/big.bin"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := mustConfig(t)
	os.WriteFile(cfg.Path, []byte("[projects.a.links]\nb = { allow = \"both\", ask = true }\n\n[projects.b]\nlink_file_mb = 1\n\n[projects.b.links]\na = \"both\"\n"), 0o600)
	if _, _, err := Save(coord, "b", "a", config.Link{Allow: config.LinkBoth, Files: config.FilesAuto}); code(err) != "human-only" {
		t.Fatalf("an agent saved: %v", err)
	}
	taken, declined, err := Save(human, "b", "a", config.Link{Allow: config.LinkBoth, Ask: true, Files: config.FilesAuto})
	if err != nil || taken != 1 || declined != 0 {
		t.Fatalf("auto: took %d, declined %d, %v", taken, declined, err)
	}
	c := mustConfig(t)
	mineB, _ := c.Links("b")
	mineA, _ := c.Links("a")
	if got := mineB["a"]; got != (config.Link{Allow: config.LinkBoth, Ask: true, Files: config.FilesAuto}) {
		t.Errorf("b's entry %+v", got)
	}
	if got := mineA["b"]; got != (config.Link{Allow: config.LinkBoth, Ask: true, Files: config.FilesAuto}) {
		t.Errorf("a's entry %+v (its ask kept)", got)
	}
	if in := transfer(t, b, small.Transfer); in.State != FileReceived {
		t.Fatalf("small %+v", in)
	}
	taken, declined, err = Save(human, "b", "a", config.Link{Allow: config.LinkOff})
	if err != nil || taken != 0 || declined != 1 {
		t.Fatalf("off: took %d, declined %d, %v", taken, declined, err)
	}
	if in := transfer(t, b, big.Transfer); in.State != FileDeclined {
		t.Fatalf("big %+v", in)
	}
	c = mustConfig(t)
	if l, _ := c.Links("a"); len(l) != 0 {
		t.Errorf("a still links %+v", l)
	}
}
