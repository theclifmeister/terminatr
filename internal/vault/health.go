package vault

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// The checks of tm vault doctor (T720, docs/SPEC.md §5.1, Vault) besides
// the links (CrossLinks): what Obsidian would do to tm's files or send
// off the machine, tm's own files changed outside tm, and files a reader
// would trip on. They change nothing; tm doctor shows them under
// "vault" once the projects folder is a vault.

// Finding is one result of Health.
type Finding struct {
	Name string `json:"name"`
	// Status is "ok", "warn" or "fail", as tm doctor's.
	Status string `json:"status"`
	Detail string `json:"detail"`
	// Files are the files it is about, by their path in the vault (at
	// most maxFiles).
	Files []string `json:"files,omitempty"`
}

const maxFiles = 20

// Rewriters are community plugins known to rewrite notes on their own
// (on open, on save or on a timer), each with what it does: tm's files
// would change behind its back.
var Rewriters = map[string]string{
	"obsidian-linter":                "Linter rewrites notes on save",
	"update-time-on-edit":            "Update time on edit writes front matter",
	"frontmatter-modified-date":      "Front matter modified date writes front matter",
	"auto-note-mover":                "Auto note mover moves notes",
	"obsidian-filename-heading-sync": "Filename heading sync renames notes",
}

// Exists reports whether the projects folder is a vault: Obsidian has
// opened it (.obsidian) or tm vault init ran.
func Exists() bool {
	root, err := home.ProjectsDir()
	if err != nil {
		return false
	}
	if fi, err := os.Stat(filepath.Join(root, obsidianDir)); err == nil && fi.IsDir() {
		return true
	}
	mark, err := onPath()
	if err != nil {
		return false
	}
	_, err = os.Lstat(mark)
	return err == nil
}

// Health runs the checks, in a fixed order.
func Health() []Finding {
	root, err := home.ProjectsDir()
	if err != nil {
		return []Finding{{Name: "vault", Status: "fail", Detail: err.Error()}}
	}
	var out []Finding
	add := func(f Finding) {
		if len(f.Files) > maxFiles {
			f.Detail += fmt.Sprintf(" (first %d of %d)", maxFiles, len(f.Files))
			f.Files = f.Files[:maxFiles]
		}
		out = append(out, f)
	}
	mark, _ := onPath()
	_, markErr := os.Lstat(mark)
	switch {
	case markErr != nil:
		add(Finding{Name: "derived notes", Status: "ok", Detail: "off (tm vault init turns them on)"})
	case !mdfile.WritesYAML() && !Migrated():
		add(Finding{Name: "derived notes", Status: "warn", Detail: "on, but front matter is still TOML: they render after the server's next start moves it to YAML"})
	default:
		add(Finding{Name: "derived notes", Status: "ok", Detail: "on"})
	}
	out = append(out, settings(root)...)
	if markErr == nil {
		add(foreign(root))
	}
	add(frontMatter())
	if Migrated() {
		files, _ := Pending()
		f := Finding{Name: "pre-YAML files", Status: "ok", Detail: "none: every live file's front matter is YAML"}
		if len(files) > 0 {
			f = Finding{Name: "pre-YAML files", Status: "warn", Detail: fmt.Sprintf("%d live files still have TOML front matter (they read; Obsidian shows none of their properties)", len(files))}
			for _, x := range files {
				f.Files = append(f.Files, x.Slug+"/"+x.Rel)
			}
		}
		add(f)
	}
	reports, _ := Unguarded()
	f := Finding{Name: "reports", Status: "ok", Detail: "every report is stored through the vault guard"}
	if len(reports) > 0 {
		f = Finding{Name: "reports", Status: "warn", Detail: fmt.Sprintf("%d reports stored before the vault guard may fetch or run something when opened in Obsidian: tm vault init puts them through it (originals kept in state/migrate/)", len(reports))}
		for _, x := range reports {
			f.Files = append(f.Files, x.Slug+"/"+x.Rel)
		}
	}
	add(f)
	return out
}

// readJSON reads .obsidian/<name> as an object; nil when it isn't there
// or isn't one.
func readJSON(root, name string) map[string]any {
	b, err := os.ReadFile(filepath.Join(root, obsidianDir, name))
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// settings checks Obsidian's settings tm cares about: Sync and Publish
// (they'd ship other projects' and servers' words off the machine),
// links updated on a rename (Obsidian would rewrite tm's files), the
// plugins that rewrite notes, and tm's keys as init left them.
func settings(root string) []Finding {
	if fi, err := os.Stat(filepath.Join(root, obsidianDir)); err != nil || !fi.IsDir() {
		return nil
	}
	var out []Finding
	core := readJSON(root, "core-plugins.json")
	var shipping []string
	for _, p := range []string{"sync", "publish"} {
		if on, _ := core[p].(bool); on {
			shipping = append(shipping, map[string]string{"sync": "Sync", "publish": "Publish"}[p])
		}
	}
	if len(shipping) > 0 {
		out = append(out, Finding{Name: "sync and publish", Status: "warn",
			Detail: "Obsidian " + strings.Join(shipping, " and ") + " on: the vault holds other projects' messages, paired servers' stubs and every report, and would leave this machine (Settings → Core plugins)"})
	} else {
		out = append(out, Finding{Name: "sync and publish", Status: "ok", Detail: "Obsidian Sync and Publish off"})
	}
	app := readJSON(root, "app.json")
	if v, ok := app["alwaysUpdateLinks"].(bool); ok && v {
		out = append(out, Finding{Name: "link auto-update", Status: "warn",
			Detail: "on: renaming a note in Obsidian rewrites links in tm's files (Settings → Files and links → Automatically update internal links: off)"})
	} else if ok {
		out = append(out, Finding{Name: "link auto-update", Status: "ok", Detail: "off"})
	} else {
		out = append(out, Finding{Name: "link auto-update", Status: "warn",
			Detail: "Obsidian's default: it may ask to rewrite links in tm's files on a rename (tm vault init turns it off)"})
	}
	var rewriting []string
	if b, err := os.ReadFile(filepath.Join(root, obsidianDir, "community-plugins.json")); err == nil {
		var ids []string
		json.Unmarshal(b, &ids)
		for _, id := range ids {
			if why, bad := Rewriters[id]; bad {
				rewriting = append(rewriting, why)
			}
		}
	}
	if len(rewriting) > 0 {
		slices.Sort(rewriting)
		out = append(out, Finding{Name: "plugins", Status: "warn", Detail: strings.Join(rewriting, "; ") + ": tm's files change behind its back (exclude the projects' folders in the plugin, or turn it off)"})
	}
	// tm's keys, as init left them.
	st := readState(root)
	var drift []string
	for _, name := range slices.Sorted(maps.Keys(st.Keys)) {
		cur := readJSON(root, name)
		if name == "types.json" {
			cur, _ = cur["types"].(map[string]any)
		}
		for _, k := range slices.Sorted(maps.Keys(st.Keys[name])) {
			if k == "alwaysUpdateLinks" {
				continue // its own line
			}
			if v, ok := cur[k]; !ok || !same(v, st.Keys[name][k]) {
				drift = append(drift, name+" "+k)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(st.Entries)) {
		cur := readJSON(root, name)
		for _, k := range slices.Sorted(maps.Keys(st.Entries[name])) {
			list, _ := cur[k].([]any)
			have := map[string]bool{}
			for _, e := range list {
				have[entryKey(e)] = true
			}
			for _, e := range st.Entries[name][k] {
				if !have[e] {
					drift = append(drift, name+" "+k+" "+e)
				}
			}
		}
	}
	switch {
	case len(st.Keys) == 0 && len(st.Entries) == 0:
		out = append(out, Finding{Name: "tm's settings", Status: "warn", Detail: "Obsidian opened the folder, tm vault init didn't run: no ignore filters, link form or Bases"})
	case len(drift) > 0:
		out = append(out, Finding{Name: "tm's settings", Status: "warn", Detail: "changed since tm vault init (it keeps your changes): " + strings.Join(drift, "; ")})
	default:
		out = append(out, Finding{Name: "tm's settings", Status: "ok", Detail: "as tm vault init left them"})
	}
	return out
}

// foreign lists the derived notes whose file isn't what tm last wrote:
// someone else's edit, which the next render overwrites.
func foreign(root string) Finding {
	f := Finding{Name: "tm's notes", Status: "ok", Detail: "every derived note is as tm wrote it"}
	file, err := ledgerPath()
	if err != nil {
		return f
	}
	l := readLedger(file)
	for _, key := range slices.Sorted(maps.Keys(l.Files)) {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(key)))
		if err == nil && hash(b) != l.Files[key] {
			f.Files = append(f.Files, key)
		}
	}
	if len(f.Files) > 0 {
		f.Status = "warn"
		f.Detail = fmt.Sprintf("%d derived notes edited outside tm: the next change overwrites them (notes for tm go in _human/)", len(f.Files))
	}
	return f
}

// frontMatter lists the files tm reads whose front matter doesn't parse:
// the live files the move to YAML covers, whatever their format now.
func frontMatter() Finding {
	f := Finding{Name: "front matter", Status: "ok", Detail: "every file tm reads parses"}
	for _, p := range projects() {
		rels := []string{"PROJECT.md", "CONTEXT.md", "MEMORY.md", "TASKS.md", "tasks/ARCHIVE.md"}
		for _, d := range []string{"memory", "inbox"} {
			ents, _ := os.ReadDir(p.Path(d))
			for _, e := range ents {
				if strings.HasSuffix(e.Name(), ".md") {
					rels = append(rels, d+"/"+e.Name())
				}
			}
		}
		recs, _ := thread.List(p)
		for _, r := range recs {
			if r.State != thread.Resolved {
				rels = append(rels, "threads/"+r.ID+"/STATUS.md")
			}
		}
		for _, rel := range rels {
			path := p.Path(filepath.FromSlash(rel))
			fi, err := os.Lstat(path)
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var m map[string]any
			if _, err := mdfile.Decode(data, &m); err != nil {
				f.Files = append(f.Files, p.Slug+"/"+rel)
			}
		}
	}
	if len(f.Files) > 0 {
		f.Status = "fail"
		f.Detail = fmt.Sprintf("%d files' front matter doesn't parse: tm can't read them (fix by hand; tm memory edit for a memory note)", len(f.Files))
	}
	return f
}
