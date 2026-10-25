package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
)

// The hash ledger, <home>/state/vault.json: what tm last wrote to each
// derived note, by its path in the vault (<slug>/<rel>), as a SHA-256.
// A note is written only when what tm renders differs from it, and a
// note the ledger has that tm no longer renders (a task archived, a
// thread gone) is removed. tm vault doctor lists notes whose file
// differs from it: someone else's edit.

type ledger struct {
	Files map[string]string `json:"files"`
	// Off: tm vault off removed the notes; a render that was waiting for
	// the lock then writes nothing while the mark is gone.
	Off bool `json:"off,omitempty"`
}

func ledgerPath() (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state", "vault.json"), nil
}

// onPath is the mark that the derived notes are on: tm vault init
// writes it and tm vault off removes it, as the human chose (docs/SPEC.md
// §5.1, Vault: derived notes only for those who open Obsidian).
func onPath() (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state", "vault"), nil
}

// On reports whether tm renders the derived notes: the mark is there and
// front matter is YAML, which the notes' properties are.
func On() bool {
	path, err := onPath()
	if err != nil || !mdfile.WritesYAML() {
		return false
	}
	_, err = os.Lstat(path)
	return err == nil
}

func hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// Render writes p's derived notes (Notes) that changed and removes those
// tm no longer renders, under the ledger's lock, and updates the ledger.
// It returns how many notes it wrote and removed.
func Render(p *project.Project) (written, removed int, err error) {
	notes, err := Notes(p)
	if err != nil {
		return 0, 0, err
	}
	file, err := ledgerPath()
	if err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return 0, 0, err
	}
	unlock, err := mdfile.Lock(file)
	if err != nil {
		return 0, 0, err
	}
	defer unlock()
	l := readLedger(file)
	if l.Off {
		if !On() {
			return 0, 0, nil // tm vault off ran while the notes rendered
		}
		l.Off = false
	}
	prefix := p.Slug + "/"
	keep := map[string]bool{}
	var errs []error
	for _, n := range notes {
		key := prefix + n.Rel
		keep[key] = true
		h := hash(n.Data)
		path := p.Path(filepath.FromSlash(n.Rel))
		if l.Files[key] == h {
			if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() {
				continue
			}
		}
		if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() {
			cur, err := os.ReadFile(path)
			switch {
			case err == nil && bytes.Equal(cur, n.Data):
				// What tm would write is there already (a canvas the
				// human changed only where tm keeps their changes).
				l.Files[key] = h
				continue
			case n.Guard && l.Files[key] == "" && !ownedFile(cur):
				// Someone else's file where tm would put this note.
				delete(keep, key)
				continue
			}
		}
		if err := writeNote(path, n.Data, !strings.HasPrefix(n.Rel, "threads/")); err != nil {
			errs = append(errs, err)
			continue
		}
		l.Files[key] = h
		written++
	}
	for _, key := range slices.Sorted(maps.Keys(l.Files)) {
		rel, ok := strings.CutPrefix(key, prefix)
		if !ok || keep[key] {
			continue
		}
		path := p.Path(filepath.FromSlash(rel))
		if derived(rel) {
			if err := os.Remove(path); err == nil {
				removed++
			} else if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
				continue
			}
			os.Remove(mdfile.LockPath(path))
		}
		delete(l.Files, key)
	}
	b, err := json.MarshalIndent(&l, "", "  ")
	if err == nil {
		err = fsx.WriteAtomic(file, append(b, '\n'), 0o600)
	}
	return written, removed, errors.Join(append(errs, err)...)
}

// ownedFile reports whether data is a note tm wrote: its tm property
// is owned.
func ownedFile(data []byte) bool {
	var f struct {
		TM string `toml:"tm" yaml:"tm"`
	}
	_, err := mdfile.Decode(data, &f)
	return err == nil && f.TM == "owned"
}

// writeNote replaces path with data under its lock, making its folder
// with mkdir (a thread's note goes only where its folder still is); a
// link in its place is replaced, never followed.
func writeNote(path string, data []byte, mkdir bool) error {
	if !mkdir {
		if fi, err := os.Lstat(filepath.Dir(path)); err != nil || !fi.IsDir() {
			return nil
		}
	} else if err := fsx.MkdirNoLink(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	unlock, err := mdfile.Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	return fsx.WriteAtomic(path, data, 0o644)
}
