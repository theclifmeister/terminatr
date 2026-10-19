package thread

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/home"
)

// A thread without a repo works in a plain folder tm made for it: no
// git, no branch, no PR (docs/SPEC.md §9). What it makes goes in its
// reports as attachments (the library); resolve removes the folder only
// once it is empty, auto-close waits while it isn't, and only resolve
// --discard throws its files away.

// Plain reports whether r works in a plain folder of tm's own: no repo,
// not adopted (an adopted folder is the user's).
func (r *Record) Plain() bool { return r.Repo == "" && !r.Adopted && !r.Checkout }

// Leftovers says what r's plain folder still holds, "" when it is empty
// or gone: nothing in it was handed in, so resolve keeps it and
// auto-close waits.
func Leftovers(dir string) (string, error) {
	files, dirs := 0, 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil && path == dir && errors.Is(err, fs.ErrNotExist):
			return fs.SkipAll
		case err != nil:
			return err
		case path == dir:
		case d.IsDir():
			dirs++
		default:
			files++
		}
		return nil
	})
	switch {
	case err != nil:
		return "", err
	case files == 1:
		return "1 file not handed in", nil
	case files > 1:
		return fmt.Sprintf("%d files not handed in", files), nil
	case dirs > 0:
		return "empty folders", nil
	}
	return "", nil
}

// OwnFolder refuses a plain folder that isn't where tm makes r's
// (WorktreeDir: ~/.terminatr/worktrees/<slug>/<id>…), so removing it
// can only remove a folder tm made: the record names the path.
func OwnFolder(slug string, r *Record) error {
	root, err := home.WorktreesDir()
	if err != nil {
		return err
	}
	dir := filepath.Clean(r.Worktree)
	base := filepath.Base(dir)
	if filepath.Dir(dir) != filepath.Join(root, slug) || base != r.ID && !strings.HasPrefix(base, r.ID+"-") {
		return fmt.Errorf("%s is not the folder tm made for %s", r.Worktree, r.ID)
	}
	if fi, err := os.Lstat(dir); err == nil && !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", r.Worktree)
	}
	return nil
}
