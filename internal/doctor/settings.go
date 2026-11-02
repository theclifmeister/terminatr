package doctor

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
)

// Settings checks config.toml for keys tm doesn't know under [keys] and
// [ui], which it ignores, for the removed agent mod's keys, which do
// nothing, and for values tm no longer has: a project
// whose complete_tasks is the removed "released" is read as "by you"
// until the user picks again in its Settings; sandbox rules in a form
// of tm 0.37 (config.OldAccess), still read.
func Settings(d Deps) []Check {
	c, err := config.Load()
	if err != nil {
		return []Check{{Group: "settings", Name: "config.toml", Status: Warn, Detail: firstLine(err.Error())}}
	}
	var out []Check
	if len(c.Unknown) > 0 {
		out = append(out, Check{Group: "settings", Name: "config.toml", Status: Warn,
			Detail: fmt.Sprintf("unknown setting %s, ignored", strings.Join(c.Unknown, ", "))})
	}
	if len(c.AgentMod) > 0 {
		out = append(out, Check{Group: "settings", Name: "config.toml", Status: Warn,
			Detail: fmt.Sprintf("%s no longer does anything: the Claude agent mod was removed; delete it", strings.Join(c.AgentMod, ", "))})
	}
	if slugs := c.Removed(); len(slugs) > 0 {
		out = append(out, Check{Group: "settings", Name: "complete tasks", Status: Warn,
			Detail: fmt.Sprintf("\"when released\" was removed, so tasks complete by you in %s: pick again in Settings (by you or when merged)", strings.Join(slugs, ", "))})
	}
	if old := c.OldAccess(); len(old) > 0 {
		out = append(out, Check{Group: "settings", Name: "access rules", Status: Warn,
			Detail: "old forms, still read: " + strings.Join(old, "; ")})
	}
	// A link names another project by its slug: one that isn't there
	// (deleted, or a typo) carries nothing (docs/SPEC.md §7.9). One on a
	// paired server, "<alias>/<slug>", needs the server paired (§16.11).
	var dangling []string
	paired := map[string]bool{}
	peers, _ := peer.LoadPeers(peer.DirIn(d.Paths.Home))
	for _, p := range peers {
		paired[p.Alias] = true
	}
	for _, slug := range c.ProjectSlugs() {
		links, _ := c.Links(slug)
		for _, other := range slices.Sorted(maps.Keys(links)) {
			if alias, _, ok := config.Remote(other); ok {
				if !paired[alias] {
					dangling = append(dangling, slug+" → "+other+" (no paired server "+alias+")")
				}
				continue
			}
			if _, err := project.Open(other); err != nil {
				dangling = append(dangling, slug+" → "+other)
			}
		}
	}
	// Linking is one act that writes both sides: entries that don't
	// mirror each other come from a hand edit (§7.9).
	var half []string
	for _, a := range c.ProjectSlugs() {
		mine, _ := c.Links(a)
		for _, b := range slices.Sorted(maps.Keys(mine)) {
			if _, _, ok := config.Remote(b); ok {
				continue // the other side is its server's
			}
			theirs, _ := c.Links(b)
			t, has := theirs[a]
			if (has && b < a) || !config.Half(mine[b], t, true, has) {
				continue // each pair once
			}
			if _, err := project.Open(b); err != nil {
				continue // dangling, below
			}
			if !has {
				half = append(half, fmt.Sprintf("%s → %s (%s has no entry)", a, b, b))
			} else {
				half = append(half, fmt.Sprintf("%s %s / %s %s", a, mine[b].Allow, b, t.Allow))
			}
		}
	}
	if len(half) > 0 {
		out = append(out, Check{Group: "settings", Name: "links", Status: Warn,
			Detail: fmt.Sprintf("half-linked: %s; set the link again in the Network popup (or tm project link) to write both sides", strings.Join(half, ", "))})
	}
	if len(dangling) > 0 {
		out = append(out, Check{Group: "settings", Name: "links", Status: Warn,
			Detail: fmt.Sprintf("linked to a project that isn't there: %s; remove the link in the Network popup", strings.Join(dangling, ", "))})
	}
	return append(out, programChecks(c, peers, paired)...)
}

// programChecks reports the programs config.toml or the projects folder
// has that can't work as they are, whether programs are on or not (the
// programs design §3.4): a name a project or a paired server has too,
// which a hand edit made (the project or server keeps it, and the
// program is left out until one is renamed), and members that aren't
// there.
func programChecks(c *config.Config, peers []peer.Peer, paired map[string]bool) []Check {
	var out []Check
	var names []string
	for _, p := range peers {
		names = append(names, p.Alias)
		if p.Name != "" {
			names = append(names, p.Name)
		}
	}
	for _, cl := range project.ProgramClashes(c, names) {
		who, how := "project", "tm project rename "+cl.Program+" NEW"
		if cl.With != "a project" {
			who, how = "paired server", "tm peer name "+cl.Program+" NEW"
		}
		out = append(out, Check{Group: "settings", Name: "programs", Status: Warn,
			Detail: fmt.Sprintf("program %s: %s is called %s too, so the program is left out and the %s keeps working; rename one (%s, or the program's [programs.%s] table and its folder projects/%s/%s)",
				cl.Program, cl.With, cl.Program, who, how, cl.Program, config.ProgramsDirName, cl.Program)})
	}
	var missing []string
	for _, slug := range c.ProgramSlugs() {
		p, _ := c.Program(slug)
		for _, m := range p.Projects {
			alias, ps, _ := config.SplitMember(m)
			switch {
			case alias != "" && !paired[alias]:
				missing = append(missing, slug+" → "+m+" (no paired server "+alias+")")
			case alias == "":
				if _, err := project.Open(ps); err != nil {
					missing = append(missing, slug+" → "+m)
				}
			}
		}
	}
	if len(missing) > 0 {
		out = append(out, Check{Group: "settings", Name: "programs", Status: Warn,
			Detail: fmt.Sprintf("a program lists a project that isn't there: %s; remove it from the program's projects", strings.Join(missing, ", "))})
	}
	return out
}
