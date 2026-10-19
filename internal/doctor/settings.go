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
// [ui], which it ignores, and for values tm no longer has: a project
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
			Detail: fmt.Sprintf("half-linked: %s; set the link again in Settings › Linked projects (or tm project link) to write both sides", strings.Join(half, ", "))})
	}
	if len(dangling) > 0 {
		out = append(out, Check{Group: "settings", Name: "links", Status: Warn,
			Detail: fmt.Sprintf("linked to a project that isn't there: %s; remove the link in Settings › Linked projects", strings.Join(dangling, ", "))})
	}
	return out
}
