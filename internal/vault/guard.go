package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// Reports stored before the vault guard (T718) are as their threads
// wrote them: HTML, remote images, embeds or runnable code blocks may
// fetch or run something when the report is opened in Obsidian. tm
// vault init puts them through the guard once, since that is when the
// human starts opening the folder there; tm vault doctor lists any left
// (a thread restored from a tarball, an init that didn't run). A report
// the guard leaves as it is isn't touched.

// Unguarded lists every project's reports (REPORT.md and reports/<n>.md,
// archived threads' too) whose text the vault guard would change.
func Unguarded() ([]File, error) {
	var out []File
	var errs []error
	for _, p := range projects() {
		var dirs []string
		for _, parent := range []string{"threads", filepath.Join("threads", "archive")} {
			ents, _ := os.ReadDir(p.Path(parent))
			for _, e := range ents {
				if e.IsDir() && thread.ValidID(e.Name()) {
					dirs = append(dirs, filepath.ToSlash(filepath.Join(parent, e.Name())))
				}
			}
		}
		for _, d := range dirs {
			rels := []string{d + "/REPORT.md"}
			ents, _ := os.ReadDir(p.Path(filepath.FromSlash(d), "reports"))
			for _, e := range ents {
				if strings.HasSuffix(e.Name(), ".md") {
					rels = append(rels, d+"/reports/"+e.Name())
				}
			}
			for _, rel := range rels {
				path := p.Path(filepath.FromSlash(rel))
				fi, err := os.Lstat(path)
				if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxReport {
					continue
				}
				data, err := os.ReadFile(path)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if string(data) != textsafe.Vault(string(data)) {
					out = append(out, File{Slug: p.Slug, Rel: rel, Path: path})
				}
			}
		}
	}
	return out, errors.Join(errs...)
}

// maxReport is the largest report the guard pass reads: a report is at
// most what tm report takes, well under it.
const maxReport = 4 << 20

// guardReports packs files into state/migrate/report-guard-<time>.tar.gz,
// then rewrites each through the vault guard and journals each
// project's count (vault.guard).
func guardReports(files []File, who caller.Caller, now time.Time) error {
	d, err := home.Dir()
	if err != nil {
		return err
	}
	backup, err := writeBackup(filepath.Join(d, "state"), "report-guard", now, files)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	done := map[string]int{}
	var errs []error
	for _, f := range files {
		data, err := os.ReadFile(f.Path)
		if err == nil {
			err = fsx.WriteAtomic(f.Path, []byte(textsafe.Vault(string(data))), 0o644)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: %w", f.Slug, f.Rel, err))
			continue
		}
		done[f.Slug]++
	}
	for _, slug := range sortedKeys(done) {
		p, err := project.Open(slug)
		if err != nil {
			continue
		}
		detail := fmt.Sprintf("%d reports stored before the vault guard put through it, originals in state/migrate/%s", done[slug], filepath.Base(backup))
		if err := p.Journal(who, "vault.guard", slug, detail); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
