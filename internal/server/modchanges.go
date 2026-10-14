package server

// What a thread's worktree changed, for user mods (docs/SPEC.md §5.3,
// thread.changes; mod API 1.8): the broker call, which tm answers by
// running git itself (package changes), and the event of the same name,
// which the mod events' look publishes for threads whose session is live
// while a mod listens to it.

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

const (
	// modChangesEvery is how often the look asks git about one thread
	// at most: its fingerprint, two git runs.
	modChangesEvery = 5 * time.Second
	// modChangesLimit bounds the look's git for one thread, so a huge
	// worktree holds the other events up no longer.
	modChangesLimit = 10 * time.Second
)

// thread.changes {project, thread, options: {mode, path, patch}} (or the
// options flat): the thread's changes since its base or its last commit.
// A path, which must be inside the worktree and pass the broker's
// refusals, narrows files to that one (the totals stay the thread's) and
// with patch adds its diff.
func (s *Server) modThreadChanges(ctx context.Context, c *modbroker.Call) (any, error) {
	var a struct {
		Thread string `json:"thread"`
		modapi.ChangesOptions
		Options *modapi.ChangesOptions `json:"options"`
	}
	p, err := modProject(c, &a)
	if err != nil {
		return nil, err
	}
	if a.Options != nil {
		a.ChangesOptions = *a.Options
	}
	mode := string(a.Mode)
	if mode != "" && mode != changes.ModeBase && mode != changes.ModeUncommitted {
		return nil, modbroker.BadParams("thread.changes: %v", changes.ErrMode)
	}
	r, err := thread.Load(p, a.Thread)
	if err != nil {
		return nil, modbroker.NotFound("%s has no thread %q", p.Slug, a.Thread)
	}
	o := changes.Options{Repo: r.Repo, Base: r.Base, Mode: mode}
	path := ""
	if a.Path != "" {
		if r.Repo == "" || r.Worktree == "" {
			return nil, modbroker.NotFound("%s has no worktree to read %s in", r.ID, a.Path)
		}
		if path, err = c.Paths.CheckIn(r.Worktree, a.Path); err != nil {
			return nil, err
		}
		if a.Patch {
			o.Path = path
		}
	}
	c.Audit("thread.changes %s %s %s", p.Slug, r.ID, strings.TrimSpace(mode+" "+path))
	cs, err := changes.Read(ctx, r.Worktree, o)
	if err != nil {
		return nil, modbroker.Failed("thread.changes: %v", err)
	}
	if path != "" {
		var one []changes.File
		for _, f := range cs.Files {
			if f.Path == path || f.Old == path {
				one = append(one, f)
			}
		}
		cs.Files = append([]changes.File{}, one...)
	}
	return cs, nil
}

// modChanges is what the look last learnt of each live thread's
// worktree, by project and thread.
type modChanges struct {
	mu   sync.Mutex
	seen map[string]*threadChanges
	// read reads a thread's changes (tests count it); nil: changes.Read.
	read func(ctx context.Context, dir string, o changes.Options) (*changes.Changeset, error)
	// now is the clock (tests); nil: time.Now.
	now func() time.Time
}

type threadChanges struct {
	at      time.Time
	fp      string
	changes *modbus.Changes
	looked  bool // in the current look
}

// of is record r's changes for the look: asked of git at most every
// modChangesEvery, read in full only when its fingerprint moved; nil
// when it has no repo or worktree.
func (m *modChanges) of(slug string, r *thread.Record, now time.Time) *modbus.Changes {
	key := slug + "/" + r.ID
	m.mu.Lock()
	if m.seen == nil {
		m.seen = map[string]*threadChanges{}
	}
	tc := m.seen[key]
	if tc == nil {
		tc = &threadChanges{}
		m.seen[key] = tc
	}
	tc.looked = true
	if !tc.at.IsZero() && now.Sub(tc.at) < modChangesEvery {
		defer m.mu.Unlock()
		return tc.changes
	}
	read, oldFP, oldCh := m.read, tc.fp, tc.changes
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), modChangesLimit)
	defer cancel()
	fp := changes.Fingerprint(ctx, r.Worktree, r.Repo)
	var ch *modbus.Changes
	if fp != "" && fp == oldFP {
		ch = oldCh
	} else if fp != "" {
		if read == nil {
			read = changes.Read
		}
		if cs, err := read(ctx, r.Worktree, changes.Options{Repo: r.Repo, Base: r.Base}); err == nil && cs.State != changes.StateNone {
			ch = &modbus.Changes{Project: slug, Thread: r.ID, Head: cs.Head, Branch: cs.Branch,
				Dirty: cs.State == changes.StateDirty, Ahead: cs.Ahead, Unpushed: cs.Unpushed, Pushed: cs.Pushed,
				Files: cs.Totals.Files, Add: cs.Totals.Add, Del: cs.Totals.Del}
		} else {
			fp = "" // read again next time
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	tc.at, tc.fp, tc.changes = now, fp, ch
	return ch
}

// sweep forgets the threads the look just ended didn't ask about, and
// starts the next look.
func (m *modChanges) sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, tc := range m.seen {
		if !tc.looked {
			delete(m.seen, k)
		}
		tc.looked = false
	}
}

// reset forgets everything (no mod listened meanwhile).
func (m *modChanges) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.seen)
}

// liveChanges is the look's thread changes for project p: of its
// unresolved threads whose session is live, while a mod listens to
// thread.changes for it; nil when none does.
func (s *Server) liveChanges(p *project.Project) func(r *thread.Record) *modbus.Changes {
	e := s.modEv
	if e == nil || !e.bus.Wants(modbus.ThreadChanges, p.Slug) {
		return nil
	}
	now := time.Now()
	if e.changes.now != nil {
		now = e.changes.now()
	}
	return func(r *thread.Record) *modbus.Changes { return e.changes.of(p.Slug, r, now) }
}
