package mdfile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/plat/flock"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// yamlFence opens and closes the YAML front matter Join writes; fence
// opens and closes TOML front matter, which Split and Decode read too.
const (
	fence     = "+++"
	yamlFence = "---"
)

// Split separates front matter from the body: TOML between "+++" lines
// or YAML between "---" lines. A file whose first line is neither has no
// front matter. An opening fence without a closing one is an error.
func Split(data []byte) (front, body []byte, err error) {
	front, body, _, err = split(data)
	return front, body, err
}

// split is Split, also saying which fence the front matter is in ("" when
// there is none).
func split(data []byte) (front, body []byte, fenced string, err error) {
	s := string(data)
	first, _, _ := strings.Cut(s, "\n")
	open := strings.TrimRight(first, "\r")
	if open != fence && open != yamlFence {
		return nil, data, "", nil
	}
	start := len(first) + 1
	for off := start; off <= len(s); {
		line, _, found := strings.Cut(s[off:], "\n")
		next := off + len(line)
		if found {
			next++
		}
		if strings.TrimRight(line, "\r") == open {
			return []byte(s[start:off]), []byte(s[next:]), open, nil
		}
		if !found {
			break
		}
		off = next
	}
	return nil, nil, "", fmt.Errorf("front matter: no closing %s line", open)
}

// Join renders front matter, as YAML (encodeYAML), and body. A nil front
// value gives a file with no front matter.
func Join(front any, body []byte) ([]byte, error) {
	var b bytes.Buffer
	if front != nil {
		y, err := encodeYAML(front)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(y) {
			// No decoder could read it back; refuse rather than write it.
			return nil, errors.New("front matter: invalid UTF-8")
		}
		b.WriteString(yamlFence + "\n")
		b.Write(y)
		b.WriteString(yamlFence + "\n")
	}
	b.Write(body)
	return b.Bytes(), nil
}

// Decode splits data and decodes the front matter, TOML or YAML, into v.
// v's fields need a toml and a yaml tag each. Keys v has no field for are
// left alone, in either format.
func Decode(data []byte, v any) (body []byte, err error) {
	front, body, f, err := split(data)
	if err != nil {
		return nil, err
	}
	if v != nil && front != nil {
		if f == yamlFence {
			err = decodeYAML(front, v)
		} else {
			_, err = toml.Decode(string(front), v)
		}
		if err != nil {
			return nil, fmt.Errorf("front matter: %w", err)
		}
	}
	return body, nil
}

// Read reads path and decodes its front matter into v.
func Read(path string, v any) (body []byte, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body, err = Decode(data, v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return body, nil
}

// LockPath is the lock file of path: a hidden ".<name>.lock" beside it,
// so locks don't clutter folders that people and agents read.
func LockPath(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".lock")
}

// Lock takes an exclusive lock on LockPath(path) and returns its release
// function. It blocks until the lock is free.
func Lock(path string) (unlock func(), err error) {
	l, err := flock.Wait(LockPath(path))
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return l.Unlock, nil
}

// Write locks path and atomically replaces it with front matter and body.
func Write(path string, front any, body []byte) error {
	data, err := Join(front, body)
	if err != nil {
		return err
	}
	unlock, err := Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	return fsx.WriteAtomic(path, data, 0o644)
}

// Update is a locked read-modify-write. fn gets the current contents (nil
// if the file doesn't exist) and returns the new contents; returning the
// same bytes, or an error, leaves the file untouched.
func Update(path string, fn func(old []byte) ([]byte, error)) error {
	unlock, err := Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := fn(old)
	if err != nil {
		return err
	}
	if bytes.Equal(old, data) {
		return nil
	}
	return fsx.WriteAtomic(path, data, 0o644)
}

// Append locks path and appends data to it, creating the file if needed.
// It is for append-only logs such as JOURNAL.md, where rewriting the whole
// file on every line would be wasteful. A link at path is refused, not
// followed (fsx.OpenNoLink).
func Append(path string, data []byte) error {
	unlock, err := Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := fsx.OpenNoLink(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
