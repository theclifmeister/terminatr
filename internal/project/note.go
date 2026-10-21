package project

import (
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

// A memory note (memory/<name>.md) may start with TOML front matter,
// as PROJECT.md does, and hold three sections (docs/SPEC.md §7.6):
//
//	+++
//	title    = "Retries stay at 4 for the gh poller"
//	touches  = ["internal/ticker/pr.go", "repo:api"]
//	standing = true
//	audience = "coordinator"
//	source   = "T481"
//	+++
//	## Decided
//	## Ruled out
//	## Watch out
//
// All of it is optional: a free-form note is a note with no front
// matter, its title its first heading.

// The sections of a note with the schema.
const (
	SectionDecided  = "Decided"
	SectionRuledOut = "Ruled out"
	SectionWatchOut = "Watch out"
)

// NoteTemplate is a new note with the schema, for the coordinator to
// copy: the skill rules and the Memory tab show it. Its comments are
// the short guide to writing one.
const NoteTemplate = `+++
# Rules, not stories: no how-we-found-out.
# No task ids, dates or (user, date) provenance in the text, except on
# a user decision someone may question.
# Each fact once, in the note whose touches fit; others link [[note]].
# Point to a repo file instead of copying its steps.
# Terse one-line imperative bullets, no hedging. Keep stop words (only,
# not): matching ignores them anyway.
# Never store what the code, git or docs already say.
# Never note a workaround for tm's own behaviour that every user would
# hit (a guard false positive, a preset that doesn't work): report it,
# for a fix in tm. Workarounds for the project's own setup (toolchain,
# tests, VMs, cloud CLIs) may stay.
title    = "One line: what was decided"
touches  = ["path/in/repo.go", "repo:<name>"]
standing = false
audience = "everyone"
source   = "T<n>"
+++
## Decided
What holds, and why.

## Ruled out
What was tried or proposed and dropped, and why.

## Watch out
What bites whoever changes this next.
`

// NoteFront is a note's front matter.
type NoteFront struct {
	Title string `toml:"title,omitempty" json:"title,omitempty"`
	// Touches are the paths (relative to a repo's root; a folder ends
	// with /) and repo:<name> entries the note is about.
	Touches []string `toml:"touches,omitempty" json:"touches,omitempty"`
	// Standing is a note every thread of the project should know.
	Standing bool `toml:"standing,omitempty" json:"standing,omitempty"`
	// Audience is who the note is for: "everyone" (the default, when
	// empty) or AudienceCoordinator, a note tm never points a thread at
	// or shows one (ForThreads).
	Audience string `toml:"audience,omitempty" json:"audience,omitempty"`
	// Source is the task or thread the note came from.
	Source string `toml:"source,omitempty" json:"source,omitempty"`
}

// AudienceCoordinator is the audience of a note for the coordinator
// only.
const AudienceCoordinator = "coordinator"

// ForThreads reports whether threads may be pointed at or shown the
// note: its audience is empty or "everyone". Any other audience, a
// typo too, keeps it from them.
func (n Note) ForThreads() bool {
	a := strings.ToLower(strings.TrimSpace(n.Audience))
	return a == "" || a == "everyone"
}

// ThreadNotes are the notes threads may be pointed at or shown
// (ForThreads), in order.
func ThreadNotes(notes []Note) []Note {
	return slices.DeleteFunc(notes, func(n Note) bool { return !n.ForThreads() })
}

// Note is a memory note, parsed.
type Note struct {
	Path string `json:"path"` // memory/<name>.md
	NoteFront
	// Body is the text after the front matter; Front is false when the
	// note has none, or front matter that doesn't decode (it is then
	// read as free-form text, all of it the body).
	Body  string `json:"-"`
	Front bool   `json:"front"`
	// titled: the front matter gives the title.
	titled bool
}

// ParseNote parses the note at path (memory/<name>.md) from its text.
// It never fails: a note whose front matter doesn't decode is read as
// free-form, so an old or broken note is still found and shown.
func ParseNote(p, text string) Note {
	n := Note{Path: p, Body: text}
	var f NoteFront
	if body, err := mdfile.Decode([]byte(text), &f); err == nil && len(body) < len(text) {
		n.NoteFront, n.Body, n.Front = f, string(body), true
		n.titled = strings.TrimSpace(f.Title) != ""
	}
	if strings.TrimSpace(n.Title) == "" {
		n.Title = noteTitle(n.Body, strings.TrimSuffix(path.Base(p), ".md"))
	}
	n.Title = strings.TrimSpace(n.Title)
	return n
}

// Section is the text under the note's "## name" heading (any level,
// case-insensitive), up to the next heading of that level or higher;
// "" when the note has none.
func (n Note) Section(name string) string {
	var out []string
	level := 0
	for _, l := range strings.Split(n.Body, "\n") {
		h := strings.TrimRight(l, "\r")
		depth := len(h) - len(strings.TrimLeft(h, "#"))
		if depth > 0 && depth <= 6 && (len(h) == depth || h[depth] == ' ') {
			if level > 0 && depth <= level {
				break
			}
			if level == 0 && strings.EqualFold(strings.TrimSpace(h[depth:]), name) {
				level = depth
				continue
			}
		}
		if level > 0 {
			out = append(out, h)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Notes reads the project's notes (memory/*.md, regular files of at
// most MaxMemoryRead bytes), sorted by title as MemoryFiles lists them.
func (p *Project) Notes() ([]Note, error) {
	ents, err := os.ReadDir(p.Path("memory"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var notes []Note
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".md") || !e.Type().IsRegular() {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.Size() > MaxMemoryRead {
			continue // gone since, or too big to read
		}
		data, err := os.ReadFile(p.Path("memory", e.Name()))
		if err != nil {
			continue
		}
		notes = append(notes, ParseNote("memory/"+e.Name(), string(data)))
	}
	slices.SortFunc(notes, func(a, b Note) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)), cmp.Compare(a.Path, b.Path))
	})
	return notes, nil
}
