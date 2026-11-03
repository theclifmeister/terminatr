package tui

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The director's info panel (the programs design §7.3, §11.2): beside a
// director's pane, as beside a coordinator's, its program: the band
// that says what waits on the user in it ("5 waiting in acme · A
// answers"), which opens the program needs dialog over the pane
// (programneeds.go), the rolled-up NEEDS YOU as tm's server ranks it
// (program.needs), then its members. Read-only, as the coordinator's
// panel is: the user acts in the dialog, never through the director.

// programInfo is what the director's panel shows of its program.
type programInfo struct {
	slug      string
	authority string
	cap       int
	paused    bool
	members   []string
	// needs is program.needs' answer, nil when tm's server didn't give
	// it (err says why).
	needs *proto.ProgramNeedsResult
	err   string
	// coords are the members' coordinators' states, by slug.
	coords map[string]string
}

// readProgramNeeds reads program slug's rolled-up needs from tm's
// server; a variable for tests.
var readProgramNeeds = func(paths tmclient.Paths, slug string) (proto.ProgramNeedsResult, error) {
	var res proto.ProgramNeedsResult
	cl, err := tmclient.Connect(paths, false)
	if err != nil {
		return res, err
	}
	defer cl.Close()
	err = cl.Call(proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: slug}, &res)
	return res, err
}

// loadProgramInfo reads director session s's program for its panel.
func loadProgramInfo(paths tmclient.Paths, s proto.SessionInfo, sessions []proto.SessionInfo) *infoData {
	pi := &programInfo{slug: s.Project, coords: map[string]string{}}
	if cfg, err := config.Load(); err == nil {
		if t, ok := cfg.Program(s.Project); ok {
			pi.authority, pi.cap, pi.paused, pi.members = t.Authority, t.ParallelThreads, t.Paused, t.Projects
		}
	}
	for _, o := range sessions {
		if o.Role == proto.RoleCoordinator {
			pi.coords[o.Project] = stateWord(o)
		}
	}
	if res, err := readProgramNeeds(paths, s.Project); err != nil {
		pi.err = errText(err)
	} else {
		pi.needs = &res
	}
	return &infoData{slug: s.Project, session: s, prog: pi}
}

// programWaiting is how many needs wait on the user in pi's program: its
// queue's rows and what its paired members' servers count.
func (pi *programInfo) waiting() int {
	if pi.needs == nil {
		return 0
	}
	n := len(pi.needs.Needs)
	for _, m := range pi.needs.Members {
		n += m.Count
	}
	return n
}

// programBand is the band's words: "5 waiting in acme".
func programBand(slug string, n int) string { return fmt.Sprintf("%d waiting in %s", n, slug) }

// programPanel is the director's panel w cells wide, with what a click
// on each line does: the band and every need open the needs dialog.
func programPanel(pi *programInfo, w int) (*panel, []infoHit) {
	pl := &panel{w: w}
	var hits []infoHit
	line := func(text string, h infoHit) {
		pl.add(text)
		hits = padHits(hits, len(pl.lines), h)
	}
	item := func(text string, h infoHit) {
		hang(pl, "", text)
		hits = padHits(hits, len(pl.lines), h)
	}
	open := infoHit{kind: hitProgramNeeds}
	line(th().Title.Render(oneLine(pi.slug))+th().Faint.Render(" · program"), infoHit{})
	about := []string{cmp.Or(pi.authority, config.AuthorityPropose)}
	if pi.cap > 0 {
		about = append(about, fmt.Sprintf("cap %d", pi.cap))
	}
	if pi.paused {
		about = append(about, "paused")
	}
	about = append(about, counted(len(pi.members), "member"))
	line(th().Faint.Render(strings.Join(about, " · ")), infoHit{})
	pl.block("needs")
	switch n := pi.waiting(); {
	case pi.needs == nil:
		pl.gap()
		hits = padHits(hits, len(pl.lines), infoHit{})
		item(th().Faint.Render("What waits on you isn't known now: "+oneLine(pi.err)), infoHit{})
	case n == 0:
		pl.gap()
		hits = padHits(hits, len(pl.lines), infoHit{})
		line(th().Faint.Render("Nothing waits for you."), infoHit{})
	default:
		pl.section("NEEDS YOU", "")
		hits = padHits(hits, len(pl.lines), infoHit{})
		g, _ := stateLook("blocked")
		item(th().Warn.Render(g+" "+programBand(pi.slug, n))+th().Faint.Render(" · A or click answers"), open)
		for _, nd := range pi.needs.Needs {
			ref := strings.TrimSpace(needRefShort(nd) + " " + needKindWords(nd))
			item(th().Head.Render(needChip(nil, nd))+" "+ref+th().Faint.Render(": "+needWhatShort(nd)), open)
		}
		for _, mb := range pi.needs.Members {
			if mb.Server != "" && mb.Count > 0 {
				item(th().Faint.Render(fmt.Sprintf("%s/%s: %d waiting on %s (answered there)", mb.Server, mb.Project, mb.Count, mb.Server)), infoHit{})
			}
		}
	}
	// The head and what waits are must-see.
	pl.mustFrom(0)
	pl.block("children")
	pl.section("CHILDREN", "")
	hits = padHits(hits, len(pl.lines), infoHit{})
	if len(pi.members) == 0 {
		line(th().Faint.Render("No members yet: the program's settings pick them."), infoHit{})
	}
	for _, m := range pi.members {
		words := ""
		switch alias, slug, _ := config.SplitMember(m); {
		case alias != "":
			words = "on " + alias
		default:
			words = "coordinator " + cmp.Or(pi.coords[slug], "not running")
		}
		item(th().Head.Render(m)+th().Faint.Render(" · "+words), infoHit{})
	}
	return pl, hits
}

// programTarget is what mods' sections in a director's panel show for:
// none of a project's, since the program is no project.
func programTarget(d *infoData) panelTarget {
	return panelTarget{slot: mod.SlotCoordinatorSection, project: d.slug}
}

// programPanelLay lays the director's panel out, with no mod sections.
func programPanelLay(d *infoData, w int, _ time.Time, o panelOpts) panelLay {
	pl, hits := programPanel(d.prog, w)
	return layPanel(pl, hits, nil, programTarget(d), o)
}

// thenProgramNeeds is the then (DashState.Then) of the director panel's
// band: the program needs dialog over the pane, Over.Project its
// program.
const thenProgramNeeds = "program-needs"

// programNeedsOver opens program slug's needs dialog over the pane,
// where this console has the dashboard; else the status bar says where
// it is.
func (c *client) programNeedsOver(slug string) {
	if !c.lock() {
		return
	}
	if !c.dashboard {
		c.flash = "the program's needs: tm program needs " + slug + ", or the dashboard"
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	c.kb = areaMain
	c.goOver(&Over{Key: thenProgramNeeds, Project: slug})
}
