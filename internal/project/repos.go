package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// The repo list's move (T335). An older tm kept each project's repo
// list in PROJECT.md's front matter, which the coordinator writes, so it
// could add any repository on disk for its threads' worktrees. The list
// now lives in config.toml ([projects.<slug>] repos), the human's file.
// MoveRepos copies the lists there once, on the first start of a server
// that has it, and leaves a mark: from then on PROJECT.md's key is never
// read again, so one a coordinator writes later approves nothing.

// movedMark is the file whose presence says the lists have moved:
// <home>/state/repos-moved, outside every project folder.
func movedMark() (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state", "repos-moved"), nil
}

// reposMoved reports whether MoveRepos has run to the end.
func reposMoved() bool {
	path, err := movedMark()
	if err != nil {
		return false
	}
	_, err = os.Lstat(path)
	return err == nil
}

// MoveRepos moves each project's repo list from PROJECT.md to
// config.toml, once: a project whose table already has a list keeps it,
// and the key goes from PROJECT.md either way. Each list moved is
// journaled (ticker project.repo.move). It leaves the mark even when a
// project's list doesn't move (the error names it): that project then
// has no repos until the user adds them, rather than PROJECT.md's,
// which its coordinator could write meanwhile. A mark that can't be
// written is an error, and the next start tries again. It returns the
// projects moved.
func MoveRepos(now time.Time) ([]string, error) {
	mark, err := movedMark()
	if err != nil {
		return nil, err
	}
	if reposMoved() {
		return nil, nil
	}
	root, err := home.ProjectsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var moved []string
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || !ValidSlug(e.Name()) {
			continue
		}
		ok, err := moveRepos(e.Name())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if ok {
			moved = append(moved, e.Name())
		}
	}
	if err := os.MkdirAll(filepath.Dir(mark), 0o700); err != nil {
		return moved, err
	}
	if err := fsx.WriteAtomic(mark, []byte("repo lists moved to config.toml "+now.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return moved, err
	}
	if err := errors.Join(errs...); err != nil {
		return moved, fmt.Errorf("not moved, so without repos until the user adds them (tm project repo add): %w", err)
	}
	return moved, nil
}

// moveRepos moves project slug's list, reporting whether it had one to
// move.
func moveRepos(slug string) (bool, error) {
	p := &Project{Slug: slug}
	p.Dir, _ = Dir(slug)
	path := p.Path("PROJECT.md")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var front struct {
		Repos *[]string `toml:"repos" yaml:"repos"`
	}
	if _, err := mdfile.Decode(data, &front); err != nil {
		return false, err
	}
	if front.Repos == nil {
		return false, nil
	}
	legacy := legacyRepos(data)
	moved := false
	err = config.UpdateRepos(slug, func(old []string, set bool) ([]string, error) {
		if set {
			return old, nil
		}
		moved = len(legacy) > 0
		return legacy, nil
	})
	if err != nil {
		return false, err
	}
	if moved {
		if err := p.Journal(caller.Caller{Kind: caller.Ticker}, "project.repo.move", slug, "to config.toml: "+strings.Join(legacy, " ")); err != nil {
			return true, err
		}
	}
	// Rewritten without the key, so PROJECT.md doesn't look like the
	// place to add one.
	err = mdfile.Update(path, func(old []byte) ([]byte, error) {
		var m Meta
		body, err := mdfile.Decode(old, &m)
		if err != nil {
			return nil, err
		}
		return mdfile.Join(m, body)
	})
	return moved, err
}
