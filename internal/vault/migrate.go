// Package vault makes the project folders a vault Obsidian reads
// (docs/SPEC.md §5.1): the one-time move of front matter from TOML
// (+++) to YAML (---), the derived notes, and tm vault's init, render,
// doctor and off.
package vault

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// The move to YAML (T716). tm wrote front matter as TOML until this
// version, which reads both and writes YAML once the move ran. Migrate
// rewrites the live files that still have TOML once, on the first
// start of a server that has it, keeping the originals in a tarball,
// and leaves a mark. From the mark on tm writes YAML (mdfile.WriteYAML:
// before it, a server older than 0.51.0 may still run), and tm update
// checks it before going back to a version that can't read YAML: such
// a tm would read a YAML TASKS.md as having no front matter and hand
// out ids from T1 again.

// YAMLSince is the first release that reads YAML front matter: tm update
// refuses to go below it once the mark exists.
const YAMLSince = "0.51.0"

// markPath is the mark that Migrate has run: <home>/state/front-yaml,
// outside every project folder.
func markPath() (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state", "front-yaml"), nil
}

// Migrated reports whether Migrate has run to the end, so files may hold
// YAML front matter only YAMLSince and later read.
func Migrated() bool {
	path, err := markPath()
	if err != nil {
		return false
	}
	_, err = os.Lstat(path)
	return err == nil
}

// File is a live file with TOML front matter.
type File struct {
	Slug string // its project
	Rel  string // its path in the project's folder, with forward slashes
	Path string // its absolute path
	// Err is why it can't be rewritten (Check only): front matter that
	// doesn't decode, or YAML that wouldn't read back the same. Such a
	// file stays TOML, which still reads.
	Err error
}

// Pending lists the live files of every project that still have TOML
// front matter: PROJECT.md, CONTEXT.md, MEMORY.md, memory/*.md,
// TASKS.md, tasks/ARCHIVE.md, open inbox items, messages and the
// STATUS.md of threads not resolved. Done inbox items, reports and
// archives are left: they read either way. The error names projects
// that couldn't be listed whole; the files found are returned anyway.
func Pending() ([]File, error) {
	root, err := home.ProjectsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []File
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || !project.ValidSlug(e.Name()) {
			continue
		}
		found, err := pending(e.Name(), filepath.Join(root, e.Name()))
		files = append(files, found...)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
		}
	}
	return files, errors.Join(errs...)
}

// pending is Pending for project slug in dir.
func pending(slug, dir string) ([]File, error) {
	rels := []string{"PROJECT.md", "CONTEXT.md", "MEMORY.md", "TASKS.md", "tasks/ARCHIVE.md"}
	for _, d := range []string{"memory", "inbox"} {
		ents, _ := os.ReadDir(filepath.Join(dir, d))
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".md") {
				rels = append(rels, d+"/"+e.Name())
			}
		}
	}
	// messages/<slug>/M-n.md and messages/@<alias>/<slug>/M-n.md.
	_ = filepath.WalkDir(filepath.Join(dir, "messages"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && strings.HasSuffix(path, ".md") {
			if rel, err := filepath.Rel(dir, path); err == nil {
				rels = append(rels, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	p := &project.Project{Slug: slug, Dir: dir}
	recs, err := thread.List(p)
	for _, r := range recs {
		if r.State != thread.Resolved {
			rels = append(rels, "threads/"+r.ID+"/STATUS.md")
		}
	}
	var files []File
	for _, rel := range rels {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
			continue // missing, or a link tm doesn't follow
		}
		if data, err := os.ReadFile(path); err == nil && mdfile.IsTOML(data) {
			files = append(files, File{Slug: slug, Rel: rel, Path: path})
		}
	}
	return files, err
}

// Check is Pending with each file's Err set when it couldn't be
// rewritten; it changes nothing (tm vault migrate --check).
func Check() ([]File, error) {
	files, err := Pending()
	for i, f := range files {
		data, rerr := os.ReadFile(f.Path)
		if rerr == nil {
			_, rerr = mdfile.ToYAML(data)
		}
		files[i].Err = rerr
	}
	return files, err
}

// Migrate rewrites every Pending file's front matter as YAML, once,
// each under its lock: first it writes the originals to
// <home>/state/migrate/front-yaml-<time>.tar.gz, then it converts them,
// journals each project's count (ticker vault.migrate) and leaves the
// mark. A file that can't be converted stays TOML, which still reads,
// and is named in the error; the mark is written regardless. Only a
// backup or a mark that can't be written leaves the move to the next
// start. It returns the number of files rewritten.
func Migrate(now time.Time) (int, error) {
	mark, err := markPath()
	if err != nil {
		return 0, err
	}
	if Migrated() {
		return 0, nil
	}
	files, listErr := Pending()
	var errs []error
	if listErr != nil {
		errs = append(errs, listErr)
	}
	n := 0
	if len(files) > 0 {
		backup, err := writeBackup(filepath.Dir(mark), "front-yaml", now, files)
		if err != nil {
			return 0, fmt.Errorf("backup: %w", err)
		}
		done := map[string]int{}
		for _, f := range files {
			if err := mdfile.Update(f.Path, func(old []byte) ([]byte, error) {
				return mdfile.ToYAML(old)
			}); err != nil {
				errs = append(errs, fmt.Errorf("%s/%s: %w", f.Slug, f.Rel, err))
				continue
			}
			done[f.Slug]++
			n++
		}
		for _, slug := range sortedKeys(done) {
			p := &project.Project{Slug: slug}
			p.Dir, _ = project.Dir(slug)
			detail := fmt.Sprintf("%d files to YAML front matter, originals in state/migrate/%s", done[slug], filepath.Base(backup))
			if err := p.Journal(caller.Caller{Kind: caller.Ticker}, "vault.migrate", slug, detail); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(mark), 0o700); err != nil {
		return n, err
	}
	if err := fsx.WriteAtomic(mark, []byte("front matter written as YAML "+now.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return n, err
	}
	if err := errors.Join(errs...); err != nil {
		return n, fmt.Errorf("left as TOML, which still reads: %w", err)
	}
	return n, nil
}

// writeBackup writes files' current contents to a new gzipped tar in
// state/migrate/, <name>-<time>.tar.gz, named <slug>/<rel> inside, and
// returns its path.
func writeBackup(state, name string, now time.Time, files []File) (string, error) {
	dir := filepath.Join(state, "migrate")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, f := range files {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return "", err
		}
		h := &tar.Header{Name: f.Slug + "/" + f.Rel, Mode: 0o600, Size: int64(len(data)), ModTime: now, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			return "", err
		}
		if _, err := tw.Write(data); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+"-"+now.UTC().Format("20060102T150405Z")+".tar.gz")
	return path, fsx.WriteAtomic(path, buf.Bytes(), 0o600)
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
