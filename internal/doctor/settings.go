package doctor

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// Settings checks config.toml for keys tm doesn't know under [keys] and
// [ui], which it ignores, and for values tm no longer has: a project
// whose complete_tasks is the removed "released" is read as "by you"
// until the user picks again in its Settings.
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
	// A link names another project by its slug: one that isn't there
	// (deleted, or a typo) carries nothing (docs/SPEC.md §7.9).
	var dangling []string
	for _, slug := range c.ProjectSlugs() {
		links, _ := c.Links(slug)
		for _, other := range slices.Sorted(maps.Keys(links)) {
			if _, err := project.Open(other); err != nil {
				dangling = append(dangling, slug+" → "+other)
			}
		}
	}
	if len(dangling) > 0 {
		out = append(out, Check{Group: "settings", Name: "links", Status: Warn,
			Detail: fmt.Sprintf("linked to a project that isn't there: %s; remove the link in Settings › Linked projects", strings.Join(dangling, ", "))})
	}
	return out
}
