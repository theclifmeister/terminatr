package filetree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
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
	case ".ssh", ".aws", ".gnupg", ".kube", ".azure", ".gcloud", ".docker", ".secrets", "secrets",
		".envrc", ".dev.vars", ".pgpass", ".htpasswd", ".vault-token", ".boto", ".s3cfg", "_netrc", ".mcp.json",
		"settings.local.json", "service-account.json", "application_default_credentials.json", "token.json", "auth.json":
		return true
	}
	if strings.HasPrefix(n, "id_") {
		return true
	}
	for _, end := range []string{".tfstate", ".tfstate.backup", ".tfvars", ".tfvars.json", ".p8", ".ppk", ".env"} {
		if strings.HasSuffix(n, end) {
			return true
		}
	}
	return false
}

// privateKey is a private key's block, its first line of body too: a program
// that only names the mark isn't one. A line's end may be written out, as in
// JSON.
var privateKey = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]{0,40}PRIVATE KEY[A-Z0-9 ]{0,10}-----(?:\\r|\\n|[\r\n \t])+[A-Za-z0-9+/=:,. -]{16,}`)

// holdsPrivateKey says text holds a private key's block.
func holdsPrivateKey(text []byte) bool {
	return bytes.Contains(text, []byte("PRIVATE KEY")) && privateKey.Match(text)
}

// hunksHoldKey says the lines hunks removed hold a private key's block.
func hunksHoldKey(hunks []proto.FilesHunk) bool {
	var text []byte
	for _, h := range hunks {
		for _, l := range h.Removed {
			text = append(append(text, l...), '\n')
		}
	}
	return holdsPrivateKey(text)
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

// hardLinkedAt says the file at p under r has other names, which may be ones
// a paired server gets nothing under, or lie outside the root. A file that
// can't be looked at counts as one.
func hardLinkedAt(r *os.Root, p string) bool {
	name := filepath.FromSlash(p)
	fi, err := r.Lstat(name)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	if !fi.Mode().IsRegular() {
		return false
	}
	f, err := fsx.OpenInNoBlock(r, name)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	defer f.Close()
	return fileLinked(f, fi)
}

// fileLinked says the open file f, fi, is a regular file with other names.
func fileLinked(f *os.File, fi fs.FileInfo) bool {
	if !fi.Mode().IsRegular() {
		return false
	}
	n, err := fsx.FileLinks(f)
	return err != nil || n > 1
}

// spelledAsStored says each element of real path rel is named as its folder
// lists it, for a system whose open file can't say where it is.
func spelledAsStored(r *os.Root, rel string) bool {
	for p := rel; p != "" && p != "."; p = path.Dir(p) {
		d, err := r.Open(filepath.FromSlash(path.Dir(p)))
		if err != nil {
			return false
		}
		names, err := d.Readdirnames(-1)
		d.Close()
		if err != nil || !slices.Contains(names, path.Base(p)) {
			return false
		}
	}
	return true
}

// withheld says a paired server gets no contents of real path rel: a name
// on the list along it, a nested checkout at or above it, or a file with
// other names.
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
	if hardLinkedAt(r, rel) {
		return true
	}
	for p := rel; p != "."; p = path.Dir(p) {
		if nestedAt(r, p) {
			return true
		}
	}
	return false
}

// holdBack marks what a far listing shows but won't open, which then says
// its name alone, and drops what lies inside such a folder. A link answers
// for what it leads to as well, git for all of them at once.
func (s *Scope) holdBack(ctx context.Context, res *proto.FilesListResult) {
	kept := res.Entries[:0]
	links := map[int]string{}
	for _, e := range res.Entries {
		e.Path = strings.TrimSuffix(e.Path, "/")
		// A folder's own listing got past the gate: a search goes deeper.
		if dir := path.Dir(e.Path); dir != "." && dir != res.Path && s.withheld(dir) {
			continue
		}
		e.Withheld = namedWithheld(e.Path)
		if !e.Withheld {
			if to, err := s.resolve(e.Path); err == nil {
				e.Withheld = s.withheld(to) || e.Link && !s.spelledTrue(to)
				if e.Link && !e.Withheld && to != "" {
					links[len(kept)] = to
				}
			}
		}
		kept = append(kept, e)
	}
	res.Entries = kept
	ign, err := s.ignoredAmong(ctx, slices.Collect(maps.Values(links)))
	for i, to := range links {
		res.Entries[i].Withheld = err != nil || ign[to]
	}
	for i := range res.Entries {
		if res.Entries[i].Withheld {
			res.Entries[i].Size, res.Entries[i].MTime = 0, time.Time{}
		}
	}
}

// spelledTrue says real path rel is where the file there says it is: a link
// can name it in any spelling the file system takes. One that doesn't open
// says no.
func (s *Scope) spelledTrue(rel string) bool {
	f, fi, err := s.open(rel)
	if err != nil {
		return false
	}
	defer f.Close()
	at, err := s.openedAt(f, fi, rel)
	return err == nil && at == rel
}

// ignoredAmong are those of paths git ignores, from one run of git.
func (s *Scope) ignoredAmong(ctx context.Context, paths []string) (map[string]bool, error) {
	if s.tree == nil || len(paths) == 0 {
		return nil, nil
	}
	// A name is a path here, not a pattern, and doesn't stop the rest:
	// check-ignore takes no literal pathspecs.
	var in strings.Builder
	for _, p := range paths {
		in.WriteString("./" + p + "\x00")
	}
	out, cut, err := s.git(ctx, gitx.Opts{OK: []int{1}, Stdin: in.String(), Max: in.Len() + 1}, "check-ignore", "-z", "--stdin")
	if err != nil {
		return nil, err
	}
	if cut {
		return nil, errors.New("too long an answer")
	}
	set := map[string]bool{}
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" {
			set[strings.TrimPrefix(p, "./")] = true
		}
	}
	return set, nil
}

// ChunkBinary says a chunk from anywhere in a file isn't text: IsBinary, but
// for a character cut at its start.
func ChunkBinary(b []byte) bool {
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

// farGate refuses a far read of clean path rel before anything says whether
// it is there: by its name, then by git and what it lies in, at where it would
// be.
func (s *Scope) farGate(ctx context.Context, rel string) error {
	if err := s.farName(rel); err != nil || !s.far || rel == "" {
		return err
	}
	p, ok := s.wouldBe(rel)
	if !ok {
		return nil
	}
	asked := []string{p}
	// A pattern for folders alone matches a path not there only as a folder;
	// a file that's there has its own answer.
	if r, err := os.OpenRoot(s.Root); err == nil {
		fi, err := r.Lstat(filepath.FromSlash(p))
		r.Close()
		if err != nil || fi.IsDir() {
			asked = append(asked, p+"/")
		}
	}
	ign, err := s.ignoredAmong(ctx, asked)
	switch {
	case len(ign) > 0:
		return fmt.Errorf("%w: %s", ErrIgnored, rel)
	case s.withheld(p):
		return fmt.Errorf("%w: %s", ErrWithheld, rel)
	case err != nil:
		return fmt.Errorf("%w: git can't say: %v", ErrIgnored, err)
	}
	return nil
}

// wouldBe is where clean path rel is or would be: the links of the part of it
// that's there followed, for git, which goes through none. false: a link leads
// out, as the read then says.
func (s *Scope) wouldBe(rel string) (string, bool) {
	for p := rel; p != "."; p = path.Dir(p) {
		real, err := s.resolve(p)
		if errors.Is(err, ErrLinkOut) {
			return "", false
		}
		if err == nil {
			return path.Join(real, strings.TrimPrefix(strings.TrimPrefix(rel, p), "/")), true
		}
	}
	return rel, true
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
