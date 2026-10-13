package view

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Tree is what the sidebar's tree folds (docs/SPEC.md §4): the active
// projects the user folded, the inactive ones the user unfolded, and the
// "inactive (n)" divider, folded by default, that the inactive projects
// hang under. Part of the view, and kept in ui.json as the tree new views
// start with.
type Tree struct {
	// Expanded are the inactive projects the user expanded, by slug:
	// their coordinator and threads show, dormant (§5.1). Not
	// "expanded", which views of protocol 6 and before saved for every
	// project.
	Expanded []string `json:"expanded_inactive,omitempty"`
	// Folded are the active projects the user folded, by slug: their row
	// alone shows, with their coordinator's state.
	Folded []string `json:"folded,omitempty"`
	// InactiveOpen unfolds the divider: the inactive projects show under
	// it.
	InactiveOpen bool `json:"inactive_open,omitempty"`
	// SectionsFolded are the mods' sidebar sections the user folded, by
	// section id (ValidSection): their header alone shows. Sections start
	// unfolded. view.tree leaves them alone; view.sections changes them
	// (since protocol 15).
	SectionsFolded []string `json:"sections_folded,omitempty"`
}

// MaxExpanded bounds the projects a view keeps expanded, and those it
// keeps folded.
const MaxExpanded = 256

// MaxSectionsFolded bounds the sections a view keeps folded; folding
// one more forgets the oldest.
const MaxSectionsFolded = 128

// sectionRE is a section's id: mod.view for a band section,
// mod.view@slug for a project's.
var sectionRE = regexp.MustCompile(`^[a-z0-9-]+\.[a-z0-9-]+(@[a-z0-9][a-z0-9-]*)?$`)

// ValidSection says whether id is a section's id: mod.view, or
// mod.view@slug for a section under project slug.
func ValidSection(id string) bool { return len(id) <= MaxKey && sectionRE.MatchString(id) }

// IsSectionFolded says whether the user folded section id.
func (t *Tree) IsSectionFolded(id string) bool { return slices.Contains(t.SectionsFolded, id) }

// FoldSections folds (on) or unfolds the sections ids, never in place;
// it says whether that changed anything. Past MaxSectionsFolded the
// oldest folds go first. Ids that aren't ValidSection are skipped.
func (t *Tree) FoldSections(ids []string, on bool) bool {
	list := slices.Clone(t.SectionsFolded)
	for _, id := range ids {
		i := slices.Index(list, id)
		switch {
		case on && i < 0 && ValidSection(id):
			list = append(list, id)
		case !on && i >= 0:
			list = slices.Delete(list, i, i+1)
		}
	}
	if n := len(list) - MaxSectionsFolded; n > 0 {
		list = list[n:]
	}
	if slices.Equal(list, t.SectionsFolded) {
		return false
	}
	t.SectionsFolded = list
	if len(list) == 0 {
		t.SectionsFolded = nil
	}
	return true
}

// IsExpanded says whether the user expanded inactive project slug.
func (t *Tree) IsExpanded(slug string) bool { return slices.Contains(t.Expanded, slug) }

// IsFolded says whether the user folded active project slug.
func (t *Tree) IsFolded(slug string) bool { return slices.Contains(t.Folded, slug) }

// Expand expands or collapses inactive project slug in the sidebar's
// tree; it says whether that changed anything.
func (t *Tree) Expand(slug string, on bool) bool { return setIn(&t.Expanded, slug, on) }

// Fold folds or unfolds active project slug; it says whether that
// changed anything.
func (t *Tree) Fold(slug string, on bool) bool { return setIn(&t.Folded, slug, on) }

// Rename follows a renamed project, its sections' folds included; it
// says whether that changed anything.
func (t *Tree) Rename(from, to string) bool {
	moved := t.IsExpanded(from) && t.Expand(from, false) && t.Expand(to, true)
	moved = t.renameSections(from, to) || moved
	return t.IsFolded(from) && t.Fold(from, false) && t.Fold(to, true) || moved
}

// renameSections moves the folds of project from's sections to to's.
func (t *Tree) renameSections(from, to string) bool {
	var list []string
	for i, id := range t.SectionsFolded {
		if base, ok := strings.CutSuffix(id, "@"+from); ok && ValidSection(base+"@"+to) {
			if list == nil {
				list = slices.Clone(t.SectionsFolded)
			}
			list[i] = base + "@" + to
		}
	}
	if list == nil {
		return false
	}
	t.SectionsFolded = list
	return true
}

// setIn adds slug to (on) or removes it from *list, never in place: a
// view handed to another goroutine shares it.
func setIn(list *[]string, slug string, on bool) bool {
	i := slices.Index(*list, slug)
	switch {
	case on && i < 0 && slug != "" && len(slug) <= MaxKey && len(*list) < MaxExpanded:
		*list = append(slices.Clone(*list), slug)
		return true
	case !on && i >= 0:
		*list = slices.Delete(slices.Clone(*list), i, i+1)
		return true
	}
	return false
}

// Valid checks a tree read from disk or the wire.
func (t *Tree) Valid() error {
	if len(t.Expanded) > MaxExpanded {
		return fmt.Errorf("too many expanded projects")
	}
	if len(t.Folded) > MaxExpanded {
		return fmt.Errorf("too many folded projects")
	}
	for _, s := range append(slices.Clone(t.Expanded), t.Folded...) {
		if s == "" || len(s) > MaxKey {
			return fmt.Errorf("a project %q in the tree", s)
		}
	}
	if len(t.SectionsFolded) > MaxSectionsFolded {
		return fmt.Errorf("too many folded sections")
	}
	for _, id := range t.SectionsFolded {
		if !ValidSection(id) {
			return fmt.Errorf("a section %q in the tree", id)
		}
	}
	return nil
}
