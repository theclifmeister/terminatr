package server

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func historyProject(t *testing.T, slug string) *project.Project {
	t.Helper()
	p, err := project.New(project.Options{Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	text := "# Tasks\n\n## Done\n\n### T1 SECRET-TITLE\nstatus: done · created: 2026-10-04 · updated: 2026-10-05\n\ndone_at: 2026-10-05\n\nSECRET-NOTES\n"
	if err := os.WriteFile(p.Tasks().TasksPath(), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPeerHistory: through a real link a paired server reads a shared
// project's numbers under the pages switch, and nothing named crosses.
func TestPeerHistory(t *testing.T) {
	testPaths(t)
	historyProject(t, "web")
	historyProject(t, "secret-proj")
	if err := config.SetProject("web", "share", true); err != nil {
		t.Fatal(err)
	}
	ex := designsExport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	off := linkedServers(t, ex)
	if !slices.Contains(off.Status().Peers[0].Caps, peer.CapPageHistory) {
		t.Fatalf("mini answers numbers, but its hello says %v", off.Status().Peers[0].Caps)
	}
	for _, slug := range []string{"web", "secret-proj", "nope"} {
		if _, err := off.History(ctx, "mini", slug); code(err) != proto.ErrPagesOff {
			t.Errorf("switch off, %s: %v, want pages-off", slug, err)
		}
	}

	on := linkedServersWith(t, ex, func(p *peer.Peer) { p.Pages = []string{"web", "secret-proj"} })
	h, err := on.History(ctx, "mini", "web")
	if err != nil || h.Done != 1 || len(h.Days) != 1 || h.Days[0].Day != "2026-10-05" {
		t.Fatalf("numbers: %+v %v", h, err)
	}
	var raw json.RawMessage
	if err := on.Call(ctx, "mini", proto.MethodPeerPagesHistory, proto.PeerPagesParams{Project: "web"}, &raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET") {
		t.Errorf("crossed: %s", raw)
	}
	if _, err := on.History(ctx, "mini", "secret-proj"); code(err) != proto.ErrNotShared {
		t.Errorf("not shared: %v, want not-shared", err)
	}
}

// TestPeerHistoryOlderPeer: a paired server on an older tm, whose hello
// lists pages but not the numbers, is never asked: no-pages, which a
// console hides on.
func TestPeerHistoryOlderPeer(t *testing.T) {
	testPaths(t)
	historyProject(t, "web")
	if err := config.SetProject("web", "share", true); err != nil {
		t.Fatal(err)
	}
	ex := designsExport(t)
	old := struct {
		peer.Exporter
		peer.PagesExporter
	}{ex, ex}
	studio := linkedServersWith(t, old, func(p *peer.Peer) { p.Pages = []string{"web"} })
	caps := studio.Status().Peers[0].Caps
	if !slices.Contains(caps, peer.CapPages) || slices.Contains(caps, peer.CapPageHistory) {
		t.Fatalf("an older exporter's hello says %v", caps)
	}
	ctx := context.Background()
	if _, err := studio.History(ctx, "mini", "web"); code(err) != proto.ErrNoPages {
		t.Errorf("numbers of an older peer: %v, want no-pages", err)
	}
	if _, err := studio.Pages(ctx, "mini", "web"); err != nil {
		t.Errorf("its pages: %v", err)
	}
	if pe := pagesError("mini", peer.ErrNoHistory); pe.Code != proto.ErrNoPages {
		t.Errorf("words: %+v", pe)
	}
}

// TestReleaseTagsMethods: release.tags reads what the setting says, and
// release.tags.set writes each mode, journaled.
func TestReleaseTagsMethods(t *testing.T) {
	testPaths(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	human := callCtx{human: true, who: func() caller.Caller { return caller.Caller{Kind: caller.Human} }}
	mustSet := func(mode string, patterns ...string) proto.ReleaseTags {
		t.Helper()
		got, perr := s.releaseTagsSet(proto.ReleaseTagsSetParams{Project: "web", Mode: mode, Patterns: patterns}, human)
		if perr != nil {
			t.Fatalf("%s: %v", mode, perr)
		}
		return got.(proto.ReleaseTags)
	}
	first, perr := s.releaseTags(proto.ReleaseTagsParams{Project: "web"})
	if perr != nil {
		t.Fatal(perr)
	}
	if r := first.(proto.ReleaseTags); r.Mode != proto.ReleaseTagsRule || r.Own || r.AllProjects != "version tags" {
		t.Errorf("before any setting: %+v", r)
	}
	if r := mustSet(proto.ReleaseTagsPatterns, "prod-*"); r.Mode != proto.ReleaseTagsPatterns || !r.Own || len(r.Patterns) != 1 {
		t.Errorf("patterns: %+v", r)
	}
	if r := mustSet(proto.ReleaseTagsNone); r.Mode != proto.ReleaseTagsNone || !r.Own {
		t.Errorf("none: %+v", r)
	}
	if r := mustSet(proto.ReleaseTagsRule); r.Mode != proto.ReleaseTagsRule || !r.Own {
		t.Errorf("own rule: %+v", r)
	}
	if r := mustSet(proto.ReleaseTagsFollow); r.Mode != proto.ReleaseTagsRule || r.Own {
		t.Errorf("follow: %+v", r)
	}
	for _, bad := range []proto.ReleaseTagsSetParams{{Project: "web", Mode: "x"}, {Project: "web", Mode: proto.ReleaseTagsPatterns}, {Project: "web", Mode: proto.ReleaseTagsPatterns, Patterns: []string{"a b"}}, {Project: "nope", Mode: proto.ReleaseTagsRule}} {
		if _, perr := s.releaseTagsSet(bad, human); perr == nil {
			t.Errorf("%+v passes", bad)
		}
	}
	p, err := project.Open("web")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p.Path("JOURNAL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "settings.release_tags") {
		t.Errorf("not journaled:\n%s", data)
	}
}
