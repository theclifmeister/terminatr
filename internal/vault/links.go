package vault

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// The cross-project link check of tm vault doctor (T724, docs/SPEC.md
// §5.1, Vault): a link from one project's note into another's folder,
// or to a paired server's stub, that points at nothing. A project
// renamed or deleted leaves them (tm project rename rewrites only the
// links it owns), and so does a server unpaired or a project no longer
// shared. Links checks the links inside each project too.

// Broken is a link that resolves to no note.
type Broken struct {
	// File is the note holding the link and Line its line (from 1), by
	// its path in the vault.
	File string `json:"file"`
	Line int    `json:"line"`
	// Target is the link's target, without a heading.
	Target string `json:"target"`
	// Why says what is missing, in fixed words.
	Why string `json:"why"`
}

// maxNote is the largest note the check reads; a bigger file is no
// note tm or a coordinator wrote (a busy board's TASKS.md is over 1 MB).
const maxNote = 8 << 20

// CrossLinks lists the broken cross-project links in every project's
// notes and the paired servers' stubs, in order of file and line.
func CrossLinks() ([]Broken, error) { return links(false) }

// Links is CrossLinks with the links inside each project too (T720): a
// vault-absolute link into its own folder that points at nothing.
func Links() ([]Broken, error) { return links(true) }

func links(inside bool) ([]Broken, error) {
	root, err := home.ProjectsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	projects := map[string]bool{}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if project.ValidSlug(e.Name()) {
			if _, err := os.Stat(filepath.Join(root, e.Name(), "PROJECT.md")); err == nil {
				projects[e.Name()] = true
				dirs = append(dirs, e.Name())
			}
		} else if e.Name() == PeersDir {
			dirs = append(dirs, e.Name())
		}
	}
	c := &checker{root: root, projects: projects, on: On(), inside: inside}
	var out []Broken
	for _, dir := range dirs {
		filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && p != filepath.Join(root, dir) || Ignored(filepath.ToSlash(rel)+"/") {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || filepath.Ext(p) != ".md" || Ignored(filepath.ToSlash(rel)) {
				return nil
			}
			out = append(out, c.file(filepath.ToSlash(rel), p, dir)...)
			return nil
		})
	}
	return out, nil
}

type checker struct {
	root     string
	projects map[string]bool
	// on: the derived notes are rendered, so a link to a task's or a
	// thread's note has a file to find.
	on bool
	// inside: links into the note's own project are checked too.
	inside bool
}

// file checks the links of the note at p, rel in the vault, in folder
// own ("" none).
func (c *checker) file(rel, p, own string) []Broken {
	fi, err := os.Stat(p)
	if err != nil || fi.Size() > maxNote {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var out []Broken
	for i, l := range linkLines(string(b)) {
		vaultlink.Each(l, func(target, _ string) {
			target, _, _ = strings.Cut(strings.TrimSpace(target), "#")
			first, _, ok := strings.Cut(target, "/")
			if !ok || first == own && !c.inside {
				return // a link in its own folder, or not vault-absolute
			}
			if why := c.why(first, target); why != "" {
				out = append(out, Broken{File: rel, Line: i + 1, Target: target, Why: why})
			}
		})
	}
	return out
}

// why says what target, a vault path starting with first, misses; ""
// when it resolves or isn't another project's or a stub.
func (c *checker) why(first, target string) string {
	if first == PeersDir {
		key := strings.TrimSuffix(strings.TrimPrefix(target, PeersDir+"/"), ".md")
		alias, _, ok := config.Remote(key)
		switch {
		case !ok:
			return "not a paired server's project"
		case c.exists(target):
			return ""
		case !c.exists(PeersDir + "/" + alias):
			return "no stubs of paired server " + alias + ": not paired, or none shared"
		}
		return "not shared by paired server " + alias
	}
	if !project.ValidSlug(first) {
		return "" // _tm, _human, .obsidian: not a project's
	}
	if !c.projects[first] {
		return "no project " + first + " (renamed or deleted?)"
	}
	if c.exists(target) || !c.on && derivedTarget(target) {
		return ""
	}
	if dir, ref := path.Split(target); dir == first+"/tasks/" && c.exists(dir+"archive/"+ref) {
		return "task archived: its note is " + dir + "archive/" + ref
	}
	return "no such note in project " + first
}

// exists reports whether the vault has target as a note or a file.
func (c *checker) exists(target string) bool {
	if strings.Contains(target, "..") {
		return false
	}
	p := filepath.Join(c.root, filepath.FromSlash(target))
	if _, err := os.Stat(p + ".md"); err == nil {
		return true
	}
	_, err := os.Stat(p)
	return err == nil
}

// derivedTarget reports whether target, <slug>/…, is a derived note's,
// which is there only while the vault is on.
func derivedTarget(target string) bool {
	_, rel, _ := strings.Cut(target, "/")
	if path.Ext(rel) != ".md" {
		rel += ".md"
	}
	return derived(rel)
}

// linkLines are the lines of s with what isn't a link in Obsidian
// blanked: fenced blocks (a message's text, examples) and code spans.
func linkLines(s string) []string {
	lines := strings.Split(s, "\n")
	var fence string
	for i, l := range lines {
		t := strings.TrimLeft(l, " \t>")
		if fence != "" {
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]+" \t") == "" {
				fence = ""
			}
			lines[i] = ""
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			c := t[0]
			fence = t[:len(t)-len(strings.TrimLeft(t, string(c)))]
			lines[i] = ""
			continue
		}
		lines[i] = dropSpans(l)
	}
	return lines
}

// dropSpans is l without its code spans.
func dropSpans(l string) string {
	if !strings.Contains(l, "`") {
		return l
	}
	var b strings.Builder
	for i := 0; i < len(l); {
		if l[i] != '`' {
			b.WriteByte(l[i])
			i++
			continue
		}
		n := len(l[i:]) - len(strings.TrimLeft(l[i:], "`"))
		end := strings.Index(l[i+n:], l[i:i+n])
		if end < 0 {
			b.WriteString(l[i : i+n])
			i += n
			continue
		}
		i += n + end + n
	}
	return b.String()
}
