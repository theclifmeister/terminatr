package thread

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// MaxMatched is how many matched memory notes a brief carries, besides
// the standing ones (docs/SPEC.md §7.1).
const MaxMatched = 3

// BriefNote is a memory note a brief carries: every standing note, then
// the best MaxMatched that Match finds for the task.
type BriefNote struct {
	project.Hit
	Standing bool `json:"standing"`
}

// Memory is what a thread's brief says of the project's memory: the
// query it matched notes against, and the notes it carries.
type Memory struct {
	Query string      `json:"query"`
	Paths []string    `json:"paths,omitempty"`
	Notes []BriefNote `json:"notes"`
}

// Counts are the standing notes and the matched ones.
func (m Memory) Counts() (standing, matched int) {
	for _, n := range m.Notes {
		if n.Standing {
			standing++
		} else {
			matched++
		}
	}
	return standing, matched
}

// BriefMemory picks the memory notes for r's brief: the standing ones,
// and the best MaxMatched others for the task's title, notes and steps
// and the paths and repos they name (TaskPaths). A project with no
// notes, or notes it can't read, gives none: a brief never fails on
// memory. A note for the coordinator only is never in one.
func BriefMemory(p *project.Project, r *Record) Memory {
	m := Memory{Query: r.Title}
	if tid := r.TaskID(); tid > 0 {
		if t, err := p.Tasks().Get(tid); err == nil {
			parts := []string{t.Title, t.Notes}
			for _, s := range t.Steps {
				parts = append(parts, s.Text)
			}
			m.Query = strings.Join(parts, "\n")
		}
	}
	roots := slices.Clone(p.Repos)
	if r.Worktree != "" {
		roots = append(roots, r.Worktree)
	}
	m.Paths = TaskPaths(m.Query, roots)
	names, said := repoNames(p.Repos), words(m.Query)
	for _, n := range names {
		if slices.Contains(said, strings.ToLower(n)) {
			m.Paths = appendNew(m.Paths, "repo:"+n)
		}
	}
	// The thread's own repo tells notes apart only when there are others.
	if r.Repo != "" && len(names) > 1 {
		m.Paths = appendNew(m.Paths, "repo:"+filepath.Base(r.Repo))
	}
	notes, err := p.Notes()
	if err != nil {
		return m
	}
	var rest []project.Note
	for _, n := range project.ThreadNotes(notes) {
		if n.Standing {
			m.Notes = append(m.Notes, BriefNote{Hit: project.Hit{Note: n}, Standing: true})
		} else {
			rest = append(rest, n)
		}
	}
	for _, h := range project.Match(rest, m.Query, m.Paths, MaxMatched) {
		m.Notes = append(m.Notes, BriefNote{Hit: h})
	}
	return m
}

// TaskPaths are the repo paths text names: words with a slash, or a file
// name with an extension (brief.go, docs/SPEC.md), in order. An absolute
// path counts only inside one of roots (the repos and the worktree), as
// the path relative to it; URLs don't count.
func TaskPaths(text string, roots []string) []string {
	var out []string
	for _, f := range strings.Fields(text) {
		w := strings.TrimFunc(f, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '/' && r != '_' && r != '.' && r != '-'
		})
		w = strings.TrimRight(w, ".")
		if w == "" || strings.Contains(w, "://") || strings.HasPrefix(w, "~") {
			continue
		}
		if filepath.IsAbs(w) {
			rel := ""
			for _, root := range roots {
				if x, err := filepath.Rel(root, w); err == nil && x != "." && !strings.HasPrefix(x, "..") {
					rel = filepath.ToSlash(x)
					break
				}
			}
			if rel == "" {
				continue
			}
			w = rel
		} else if !strings.Contains(w, "/") && !fileName(w) {
			continue
		}
		out = appendNew(out, w)
	}
	return out
}

// fileName reports whether w looks like a file name with an extension:
// a name of two or more characters with a letter, a dot, and one to
// five letters and digits with a letter (go.mod, brief.md; not e.g.,
// 0.31.0 or v1.2).
func fileName(w string) bool {
	i := strings.LastIndexByte(w, '.')
	if i < 2 || len(w)-i-1 < 1 || len(w)-i-1 > 5 {
		return false
	}
	base, ext := w[:i], w[i+1:]
	return hasLetter(base) && hasLetter(ext) && strings.IndexFunc(ext, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) < 0
}

func hasLetter(s string) bool { return strings.IndexFunc(s, unicode.IsLetter) >= 0 }

// repoNames are the repos' folder names, the <name> of repo:<name>.
func repoNames(repos []string) []string {
	var out []string
	for _, r := range repos {
		out = appendNew(out, filepath.Base(filepath.Clean(r)))
	}
	return out
}

func words(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.'
	}) {
		if w = strings.Trim(w, ".-_"); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func appendNew(s []string, v string) []string {
	if v == "" || slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}

// Render is the brief's "## Memory notes" section, within ResetBudget:
// each note under its title, path and why it is there, with its text
// (front matter left out) clipped to an equal share; "" for no notes.
// With project slug, each note's path is a link to it (vaultlink), as
// brief.md has it; "" keeps it plain, for a terminal.
func (m Memory) Render(dir, slug string) string {
	if len(m.Notes) == 0 {
		return ""
	}
	head := "## Memory notes\n\nNotes from the project's memory for this task: standing ones every thread gets,\n" +
		"matched ones by the task's words and paths. Each is cut to fit; the full note is in\n" +
		filepath.Join(dir, "memory") + "/ (and `tm memory find` looks for more).\n\n"
	share := (ResetBudget - len(head)) / len(m.Notes)
	var b strings.Builder
	b.WriteString(head)
	for _, n := range m.Notes {
		why := "standing"
		if !n.Standing {
			why = "matched: " + strings.Join(append(slices.Clone(n.Terms), n.Paths...), ", ")
		}
		path := n.Note.Path
		if slug != "" {
			path = vaultlink.Link(slug, vaultlink.Memory, path, path)
		}
		title := fmt.Sprintf("### %s (%s; %s)\n", oneLine(n.Note.Title), path, why)
		body := noteText(n.Note)
		if body != "" {
			body += "\n"
		}
		b.WriteString(Clip(title+body, share) + "\n")
	}
	return Clip(strings.TrimRight(b.String(), "\n")+"\n", ResetBudget)
}

// noteText is a note's body, less a first heading that only repeats its
// title, with headings below ### so they nest under the note's.
func noteText(n project.Note) string {
	lines := strings.Split(strings.TrimSpace(n.Body), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#") &&
		strings.EqualFold(strings.TrimSpace(strings.TrimLeft(lines[0], "#")), n.Title) {
		lines = lines[1:]
	}
	for i, l := range lines {
		depth := len(l) - len(strings.TrimLeft(l, "#"))
		if depth > 0 && depth < 4 && (len(l) == depth || l[depth] == ' ') {
			lines[i] = "####" + l[depth:]
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
