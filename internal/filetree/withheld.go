package filetree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// SecretName says a file's name is one that usually holds a secret. On
// this machine it is a hint for the eye: a console says so as it opens the
// file. For a paired server it is a rule, with more names (withheldName).
func SecretName(name string) bool {
	n := strings.ToLower(name)
	if n == ".env" || strings.HasPrefix(n, ".env.") {
		for _, safe := range []string{".example", ".sample", ".template", ".dist"} {
			if strings.HasSuffix(n, safe) {
				return false
			}
		}
		return true
	}
	switch n {
	case ".netrc", ".npmrc", ".pypirc", ".git-credentials", "credentials", "credentials.json",
		"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	switch path.Ext(n) {
	case ".pem", ".key", ".p12", ".pfx", ".keystore", ".jks", ".kdbx":
		return true
	}
	return strings.HasPrefix(n, "secrets.") || strings.HasPrefix(n, "secret.")
}

// withheldName says a paired server gets no contents under this name:
// SecretName's, however the file system might spell them (any case,
// Windows' dropped trailing dots and spaces), and the folders and state
// files that hold keys.
func withheldName(name string) bool {
	n := strings.ToLower(strings.TrimRight(name, ". "))
	if SecretName(name) || SecretName(n) {
		return true
	}
	switch n {
	case ".ssh", ".aws", ".gnupg", ".kube":
		return true
	}
	return strings.HasSuffix(n, ".tfstate") || strings.HasSuffix(n, ".tfstate.backup")
}

// namedWithheld says an element of slash path rel is withheldName.
func namedWithheld(rel string) bool {
	return rel != "" && slices.ContainsFunc(strings.Split(rel, "/"), withheldName)
}

// nestedAt says folder p under r is the top of a checkout of its own: its
// repository's ignore rules aren't the root's. What can't be looked at
// counts as one.
func nestedAt(r *os.Root, p string) bool {
	fi, err := r.Lstat(filepath.FromSlash(p))
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	if !fi.IsDir() {
		return false
	}
	_, err = r.Lstat(filepath.Join(filepath.FromSlash(p), ".git"))
	return !errors.Is(err, fs.ErrNotExist)
}

// withheld says a paired server gets no contents of real path rel: a name
// on the list along it, or a nested checkout at or above it.
func (s *Scope) withheld(rel string) bool {
	if rel == "" {
		return false
	}
	if namedWithheld(rel) {
		return true
	}
	r, err := os.OpenRoot(s.Root)
	if err != nil {
		return true
	}
	defer r.Close()
	for p := rel; p != "."; p = path.Dir(p) {
		if nestedAt(r, p) {
			return true
		}
	}
	return false
}

// holdBack marks what a far listing shows but won't open, which then says
// its name alone, and drops what lies inside such a folder.
func (s *Scope) holdBack(ctx context.Context, res *proto.FilesListResult) {
	kept := res.Entries[:0]
	for _, e := range res.Entries {
		e.Path = strings.TrimSuffix(e.Path, "/")
		// A folder's own listing got past the gate: a search goes deeper.
		if dir := path.Dir(e.Path); dir != "." && dir != res.Path && s.withheld(dir) {
			continue
		}
		e.Withheld = namedWithheld(e.Path)
		if !e.Withheld {
			// A link answers for what it leads to as well.
			if to, err := s.resolve(e.Path); err == nil {
				e.Withheld = s.withheld(to)
				if e.Link && !e.Withheld {
					ign, err := s.isIgnored(ctx, to)
					e.Withheld = ign || err != nil
				}
			}
		}
		if e.Withheld {
			e.Size, e.MTime = 0, time.Time{}
		}
		kept = append(kept, e)
	}
	res.Entries = kept
}

// chunkBinary says a chunk from anywhere in a file isn't text: IsBinary, but
// for a character cut at its start.
func chunkBinary(b []byte) bool {
	for i := 0; i < utf8.UTFMax-1 && len(b) > 0 && !utf8.RuneStart(b[0]); i++ {
		b = b[1:]
	}
	return IsBinary(b)
}

// farName refuses a far read of a path whose own name is withheld, before
// anything says whether it is there.
func (s *Scope) farName(rel string) error {
	if s.far && namedWithheld(rel) {
		return fmt.Errorf("%w: %s", ErrWithheld, rel)
	}
	return nil
}

// farIgnored is what a far stamp leaves out; nil when git can't say all of
// it, and no folder then answers. A near scope has none.
func (s *Scope) farIgnored(ctx context.Context) map[string]bool {
	if !s.far {
		return nil
	}
	ign, cut, err := s.ignoredUnder(ctx, "")
	if err != nil || cut {
		return nil
	}
	return ign
}
