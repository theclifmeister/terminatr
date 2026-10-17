package project

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Kinds of a Conflict.
const (
	ConflictDecided  = "decided"
	ConflictRuledOut = "ruled-out"
)

// MaxConflicts is the most Conflicts answers.
const MaxConflicts = 10

// FileDiff is one changed file of a diff: its path (relative to the
// repo's root), a renamed file's old path, and its unified patch.
type FileDiff struct {
	Path  string
	Old   string
	Patch string
}

// Conflict is a change a note's decision speaks against: a number the
// note decided that the diff replaces (Decided, Changed), or words the
// note ruled out that the diff adds (Terms).
type Conflict struct {
	Note    string   `json:"note"`
	Title   string   `json:"title"`
	File    string   `json:"file"`
	Kind    string   `json:"kind"`
	Decided string   `json:"decided,omitempty"`
	Changed string   `json:"changed,omitempty"`
	Terms   []string `json:"terms,omitempty"`
}

func (c Conflict) String() string {
	if c.Kind == ConflictDecided {
		return fmt.Sprintf("%s changes %s to %s; %s (%s) decided %s", c.File, c.Decided, c.Changed, c.Note, c.Title, c.Decided)
	}
	return fmt.Sprintf("%s adds %s; %s (%s) ruled it out", c.File, strings.Join(c.Terms, ", "), c.Note, c.Title)
}

// TouchesPath reports whether the note touches path in the repo named repo
// (a repo:<name> entry touches every path of that repo).
func (n Note) TouchesPath(repo, path string) bool {
	return slices.ContainsFunc(n.Touches, func(t string) bool {
		if name, ok := strings.CutPrefix(strings.TrimSpace(t), "repo:"); ok {
			return repo != "" && name == repo
		}
		return pathsMeet(path, t)
	})
}

// Conflicts checks a diff of the repo named repo against the notes that
// touch its files, deterministically, and answers at most MaxConflicts,
// by note and file. Within each change (a run of removed lines and the
// added lines after it) of a touched file, a number of the note's
// Decided section that the removed lines have and the added lines
// don't, while the added lines bring a number the removed lines lack,
// is a change of a decided value. In a touched file's added lines, the
// words of its Ruled out section (four or more letters, not in its
// title or Decided section) that its removed lines lack are ruled-out
// words: two of them (one, when the section has only one) are a
// conflict. Identifiers count by their parts (backoffJitter: backoff,
// jitter).
func Conflicts(notes []Note, repo string, diffs []FileDiff) []Conflict {
	var out []Conflict
	for _, n := range notes {
		decided, ruled := numbers(n.Section(SectionDecided)), ruledTerms(n)
		if len(decided) == 0 && len(ruled) == 0 {
			continue
		}
		for _, d := range diffs {
			if !n.TouchesPath(repo, d.Path) && (d.Old == "" || !n.TouchesPath(repo, d.Old)) {
				continue
			}
			blocks := changeBlocks(d.Patch)
			c := Conflict{Note: n.Path, Title: n.Title, File: d.Path}
			for _, b := range blocks {
				if c.Decided != "" {
					break
				}
				was, now := numbers(strings.Join(b.removed, "\n")), numbers(strings.Join(b.added, "\n"))
				for _, v := range decided {
					if !slices.Contains(was, v) || slices.Contains(now, v) {
						continue
					}
					if i := slices.IndexFunc(now, func(x string) bool { return !slices.Contains(was, x) }); i >= 0 {
						c.Decided, c.Changed = v, now[i]
						break
					}
				}
			}
			if c.Decided != "" {
				c.Kind = ConflictDecided
				out = append(out, c)
			}
			if len(ruled) == 0 {
				continue
			}
			var added, removed []string
			for _, b := range blocks {
				added, removed = append(added, b.added...), append(removed, b.removed...)
			}
			now, was := identSet(strings.Join(added, "\n")), identSet(strings.Join(removed, "\n"))
			var hit []string
			for _, t := range ruled {
				if now[t] && !was[t] {
					hit = append(hit, t)
				}
			}
			if len(hit) > 0 && len(hit) >= min(2, len(ruled)) {
				out = append(out, Conflict{Note: n.Path, Title: n.Title, File: d.Path, Kind: ConflictRuledOut, Terms: hit})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Conflict) int {
		return cmp.Or(cmp.Compare(a.Note, b.Note), cmp.Compare(a.File, b.File))
	})
	if len(out) > MaxConflicts {
		out = out[:MaxConflicts]
	}
	return out
}

// ruledTerms are the words of a note's Ruled out section to look for:
// four or more letters, not a number or a stop word, not in its title
// or Decided, each once.
func ruledTerms(n Note) []string {
	known := wordSet(n.Title + "\n" + n.Section(SectionDecided))
	var out []string
	for _, t := range words(n.Section(SectionRuledOut)) {
		if len([]rune(t)) >= 4 && !known[t] && !stopWord(t) && strings.ContainsFunc(t, unicode.IsLetter) && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// block is one change of a patch: removed lines, then the added ones.
type block struct{ removed, added []string }

// changeBlocks splits a unified patch into its changes, without the
// file headers.
func changeBlocks(patch string) []block {
	var out []block
	var cur block
	flush := func() {
		if len(cur.removed)+len(cur.added) > 0 {
			out = append(out, cur)
		}
		cur = block{}
	}
	inHunk := false
	for _, l := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			flush()
			inHunk = true
		case !inHunk:
		case strings.HasPrefix(l, "-"):
			if len(cur.added) > 0 {
				flush()
			}
			cur.removed = append(cur.removed, l[1:])
		case strings.HasPrefix(l, "+"):
			cur.added = append(cur.added, l[1:])
		case strings.HasPrefix(l, `\`):
		case strings.HasPrefix(l, "diff "):
			flush()
			inHunk = false
		default:
			flush()
		}
	}
	flush()
	return out
}

// numbers are the numbers in s, each once, in order: runs of digits
// with an optional decimal part, not part of a word or version (T481,
// v0.30), though a unit may follow (30s).
func numbers(s string) []string {
	var out []string
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if !unicode.IsDigit(r[i]) || i > 0 && (unicode.IsLetter(r[i-1]) || unicode.IsDigit(r[i-1]) || r[i-1] == '_' || r[i-1] == '.') {
			continue
		}
		j := i
		for j < len(r) && unicode.IsDigit(r[j]) {
			j++
		}
		if j+1 < len(r) && r[j] == '.' && unicode.IsDigit(r[j+1]) {
			for j++; j < len(r) && unicode.IsDigit(r[j]); j++ {
			}
			if j < len(r) && r[j] == '.' {
				for j < len(r) && (r[j] == '.' || unicode.IsDigit(r[j])) {
					j++
				}
				i = j
				continue // 1.2.3, a version
			}
		}
		if n := string(r[i:j]); !slices.Contains(out, n) {
			out = append(out, n)
		}
		i = j
	}
	return out
}

// identSet is the set of s's words, lowercased, identifiers split into
// their parts as well (backoffJitter, max_retries) and kept whole.
func identSet(s string) map[string]bool {
	set := wordSet(s)
	var part []rune
	add := func() {
		if len(part) > 0 {
			set[strings.ToLower(string(part))] = true
		}
		part = part[:0]
	}
	r := []rune(s)
	for i, c := range r {
		switch {
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			add()
			continue
		case unicode.IsUpper(c) && i > 0 && (unicode.IsLower(r[i-1]) || i+1 < len(r) && unicode.IsUpper(r[i-1]) && unicode.IsLower(r[i+1])):
			add()
		}
		part = append(part, c)
	}
	add()
	return set
}
