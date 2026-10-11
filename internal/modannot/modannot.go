// Package modannot is the annotation store of user mods (the mods design
// §5, Annotations): badges a mod shows by a task or a thread, kept in the
// mod's own folder, never in TASKS.md. $.annotate writes it through the
// broker; the consoles read it to draw the badges.
//
// The file is the mod's, so a reader trusts none of it: Read cleans every
// span again and keeps the limits.
package modannot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// File is the store's name in a mod's own folder.
const File = "annotations.json"

// The limits of one mod's annotations.
const (
	MaxKey     = 32   // bytes of a key
	MaxSpans   = 6    // spans in one annotation
	MaxText    = 32   // runes in one span
	MaxKeys    = 8    // annotations on one target
	MaxTargets = 2000 // targets with an annotation, in all projects
	MaxBytes   = 1 << 20
)

// Roles are the theme roles a span may name; the same as the mod API's
// (modapi.Roles, checked by a test).
var Roles = []string{"head", "sel", "faint", "accent", "title", "bad", "warn", "good", "info", "plain"}

// Span is a run of text in a theme role.
type Span struct {
	Text string `json:"text"`
	Role string `json:"role,omitempty"`
}

// Badge is one annotation of a mod on a target, as a console draws it.
type Badge struct {
	Mod   string
	Key   string
	Spans []Span
}

// Task and Thread name the target of a task or a thread in the store.
func Task(ref string) string  { return "task:" + ref }
func Thread(id string) string { return "thread:" + id }

// data is the file: per project, per target, per key.
type data struct {
	Projects map[string]map[string]map[string][]Span `json:"projects"`
}

var mu sync.Mutex // one write at a time; the server is the only writer

// CheckKey refuses a key that is not 1 to MaxKey of [a-z0-9._-].
func CheckKey(key string) error {
	if key == "" || len(key) > MaxKey {
		return fmt.Errorf("a key is 1 to %d bytes", MaxKey)
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return fmt.Errorf("key %q: use a-z, 0-9, . _ -", key)
		}
	}
	return nil
}

// Clean makes spans safe to draw: each text one line of at most MaxText
// runes with its control characters (escape sequences included) made
// spaces, an unknown role plain, empty spans dropped. It refuses more than
// MaxSpans.
func Clean(spans []Span) ([]Span, error) {
	if len(spans) > MaxSpans {
		return nil, fmt.Errorf("%d spans, over %d", len(spans), MaxSpans)
	}
	var out []Span
	for _, s := range spans {
		t := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || r == utf8.RuneError {
				return ' '
			}
			return r
		}, s.Text)), " ")
		if rs := []rune(t); len(rs) > MaxText {
			t = string(rs[:MaxText-1]) + "…"
		}
		if t == "" {
			continue
		}
		role := s.Role
		if !slices.Contains(Roles, role) {
			role = ""
		}
		out = append(out, Span{Text: t, Role: role})
	}
	return out, nil
}

func read(dir string) (data, error) {
	d := data{Projects: map[string]map[string]map[string][]Span{}}
	b, err := os.ReadFile(filepath.Join(dir, File))
	if errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if len(b) > MaxBytes {
		return d, fmt.Errorf("%s is over %d bytes", File, MaxBytes)
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return data{Projects: map[string]map[string]map[string][]Span{}}, fmt.Errorf("%s: %w", File, err)
	}
	if d.Projects == nil {
		d.Projects = map[string]map[string]map[string][]Span{}
	}
	return d, nil
}

// Set puts mod folder dir's annotation key on target of project: spans
// replace the old ones, and none (or only empty ones) removes the
// annotation. It reports whether the store changed. Spans must be Clean.
func Set(dir, project, target, key string, spans []Span) (bool, error) {
	if err := CheckKey(key); err != nil {
		return false, err
	}
	mu.Lock()
	defer mu.Unlock()
	d, err := read(dir)
	if err != nil {
		// A broken file is the mod's own; start it again.
		d = data{Projects: map[string]map[string]map[string][]Span{}}
	}
	targets := d.Projects[project]
	old := targets[target][key]
	if len(spans) == 0 {
		if _, ok := targets[target][key]; !ok {
			return false, nil
		}
		delete(targets[target], key)
		if len(targets[target]) == 0 {
			delete(targets, target)
		}
		if len(targets) == 0 {
			delete(d.Projects, project)
		}
	} else {
		if slices.Equal(old, spans) {
			return false, nil
		}
		if targets == nil {
			targets = map[string]map[string][]Span{}
			d.Projects[project] = targets
		}
		keys := targets[target]
		if keys == nil {
			if n := count(d); n >= MaxTargets {
				return false, fmt.Errorf("%d targets annotated, over %d", n, MaxTargets)
			}
			keys = map[string][]Span{}
			targets[target] = keys
		}
		if _, ok := keys[key]; !ok && len(keys) >= MaxKeys {
			return false, fmt.Errorf("%s has %d annotations, over %d", target, len(keys), MaxKeys)
		}
		keys[key] = spans
	}
	b, err := json.Marshal(d)
	if err != nil {
		return false, err
	}
	if len(b) > MaxBytes {
		return false, fmt.Errorf("the annotations would be %d bytes, over %d", len(b), MaxBytes)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	return true, fsx.WriteAtomic(filepath.Join(dir, File), b, 0o600)
}

func count(d data) int {
	n := 0
	for _, t := range d.Projects {
		n += len(t)
	}
	return n
}

// Read is mod id's annotations in project, by target (Task, Thread),
// from mod folder dir; a missing or broken file has none. Everything is
// cleaned again, and what is over the limits is dropped.
func Read(dir, id, project string) map[string][]Badge {
	d, err := read(dir)
	if err != nil {
		return nil
	}
	out := map[string][]Badge{}
	for target, keys := range d.Projects[project] {
		names := make([]string, 0, len(keys))
		for k := range keys {
			if CheckKey(k) == nil {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		for _, k := range names {
			if len(out[target]) >= MaxKeys {
				break
			}
			spans, err := Clean(keys[k])
			if err != nil || len(spans) == 0 {
				continue
			}
			out[target] = append(out[target], Badge{Mod: id, Key: k, Spans: spans})
		}
	}
	return out
}
