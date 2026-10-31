package peer

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// designsExport is pagesExport serving web's designs too.
type designsExport struct {
	*pagesExport
}

func (f *designsExport) Designs(c caller.Caller, slug string) (proto.PeerDesigns, error) {
	return proto.PeerDesigns{Designs: []proto.PeerDesign{{Name: "vault", Title: "Vault", Status: "planned"}}}, nil
}

// TestPeerDesignsGate: peer.pages.designs answers only while the pages
// switch is on for its project, pages-off otherwise; an exporter of
// pages without designs has no such method, and its hello says pages
// but not page-designs.
func TestPeerDesignsGate(t *testing.T) {
	ex := &designsExport{pagesExport: &pagesExport{fakeExport: newFakeExport()}}
	var on []string
	a := pagesPair(t, ex, &on)
	ctx := context.Background()
	if err := a.call(ctx, proto.MethodPeerPagesDesigns, proto.PeerPagesParams{Project: "web"}, nil); code(err) != proto.ErrPagesOff {
		t.Errorf("off: %v, want pages-off", err)
	}
	on = []string{"api"}
	if err := a.call(ctx, proto.MethodPeerPagesDesigns, proto.PeerPagesParams{Project: "web"}, nil); code(err) != proto.ErrPagesOff {
		t.Errorf("on for another project: %v, want pages-off", err)
	}
	on = []string{"web"}
	var ds proto.PeerDesigns
	if err := a.call(ctx, proto.MethodPeerPagesDesigns, proto.PeerPagesParams{Project: "web"}, &ds); err != nil || len(ds.Designs) != 1 {
		t.Errorf("on: %+v %v", ds, err)
	}
	pagesOnly := pagesPair(t, &pagesExport{fakeExport: newFakeExport()}, &on)
	if err := pagesOnly.call(ctx, proto.MethodPeerPagesDesigns, proto.PeerPagesParams{Project: "web"}, nil); code(err) != proto.ErrUnknownMethod {
		t.Errorf("pages without designs: %v, want unknown-method", err)
	}
	with := &Manager{opts: Options{Export: ex}}
	without := &Manager{opts: Options{Export: &pagesExport{fakeExport: newFakeExport()}}}
	if !slices.Contains(with.caps(), CapPageDesigns) || slices.Contains(without.caps(), CapPageDesigns) || !slices.Contains(without.caps(), CapPages) {
		t.Errorf("caps: %v and %v", with.caps(), without.caps())
	}
	if n := len(with.caps()); n > maxCaps {
		t.Errorf("%d caps, over the %d a hello keeps", n, maxCaps)
	}
}

// TestDesignsAreCleaned: a peer's designs arrive with no escape in any
// string, every one on one line, names and ids checked (a bad name, a
// repeated one and a bad build task id dropped; a bad design task
// blanked), a title for each, and each list cut to its most.
func TestDesignsAreCleaned(t *testing.T) {
	in := proto.PeerDesigns{Designs: []proto.PeerDesign{
		{Name: "vault", Title: "the\u202e vault\nnow" + osc52, Status: "build ing", Task: "../T1", TaskStatus: "review", Page: true, TasksDone: -3,
			Answers: []string{"a\nb" + osc52, ""}, Build: []proto.PeerDesignTask{
				{ID: "T2", Title: "x\x1b[2Jy", Status: "done", After: []string{"T1", "nope"}, Working: true, Archived: true},
				{ID: "../T3", Title: "bad"},
				{ID: "T4", Title: "runs", Status: "started", Working: true},
			}},
		{Name: "../etc", Title: "path"},
		{Name: "vault", Title: "again"},
		{Name: "untitled"},
	}}
	for range proto.PeerDesignAnswers + 3 {
		in.Designs[0].Answers = append(in.Designs[0].Answers, "x")
	}
	for i := range proto.PeerDesignsMax + 5 {
		in.Designs = append(in.Designs, proto.PeerDesign{Name: fmt.Sprintf("d%d", i)})
	}
	out := cleanDesigns(in)
	if hasEscape(t, out) {
		t.Fatalf("an escape crossed")
	}
	if len(out.Designs) != proto.PeerDesignsMax || !out.Truncated {
		t.Fatalf("designs: %d, truncated %v", len(out.Designs), out.Truncated)
	}
	d := out.Designs[0]
	if d.Name != "vault" || strings.Contains(d.Title, "\n") || d.Status != "" || d.Task != "" || d.TaskStatus != "review" || !d.Page || d.TasksDone != 0 {
		t.Errorf("design: %+v", d)
	}
	if len(d.Answers) != proto.PeerDesignAnswers || strings.Contains(d.Answers[0], "\n") {
		t.Errorf("answers: %d %q", len(d.Answers), d.Answers[0])
	}
	if len(d.Build) != 2 || d.Build[0].ID != "T2" || d.Build[0].Working || !slices.Equal(d.Build[0].After, []string{"T1"}) ||
		strings.Contains(d.Build[0].Title, "\x1b") || d.Build[1].ID != "T4" || !d.Working {
		t.Errorf("build: %+v", d.Build)
	}
	if out.Designs[1].Name != "untitled" || out.Designs[1].Title != "untitled" {
		t.Errorf("second: %+v", out.Designs[1])
	}
}
