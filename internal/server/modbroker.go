package server

// The user mods' broker (docs/SPEC.md §5.3, Broker; package modbroker):
// a code mod's host asks for everything through it. The broker checks
// the mod's grants and does the call; here the server adds the methods
// that read its projects (project_read) and raise inbox items (inbox),
// and gives the broker the registry as it is now and the mod's log.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modannot"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// modRegistryAge is how long the broker trusts the registry without
// looking at its files' stamp (modsStamp): a grant the human takes back
// applies within it.
const modRegistryAge = time.Second

// modCallsAtOnce is how many of one mod's calls the broker works on at
// once; up to modCallsWaiting more wait their turn, and a call past
// those is refused (CodeFailed) until answers come back.
const (
	modCallsAtOnce  = 8
	modCallsWaiting = 56
)

// Inbox limits for a mod: an item's subject and summary are clipped, and
// a mod with this many unhandled items in a project raises no more.
const (
	modSubjectMax  = 80
	modSummaryMax  = 1000
	modItemsUnread = 20
)

// modBroker is the server's broker, made on first use.
type modBroker struct {
	once sync.Once
	b    *modbroker.Broker

	// mu guards the installed mods as last read (installedMods): reg, read from
	// files whose stamp was stamp, which was last looked at at checked;
	// cfgOK is whether config.toml loaded then.
	mu      sync.Mutex
	reg     *mod.Registry
	stamp   string
	checked time.Time
	cfgOK   bool
	// logMu serialises writes to the mods' logs.
	logMu sync.Mutex
	// busy holds a slot per call each mod has running (modCallsAtOnce);
	// inFlight counts each mod's calls running or waiting for a slot.
	busy     map[string]chan struct{}
	inFlight map[string]int
}

// CallMod answers mod id's call of method from its host (the supervisor
// passes each $ request here). The result is sent to the mod as JSON; an
// error is a *modbroker.Error with its code.
func (s *Server) CallMod(ctx context.Context, id, method string, params json.RawMessage) (any, error) {
	return s.broker().Call(ctx, id, method, params)
}

// ModRequest answers request m from mod id's host on conn: the
// supervisor's Broker hook (modsup.Options.Broker). It returns at once;
// the call runs on its own goroutine, at most modCallsAtOnce of a mod's
// at a time, so a slow fetch never holds up the host's events. A call
// past modCallsAtOnce+modCallsWaiting is refused, and ctx's end (the
// host's) ends the mod's calls. A notification (no id) is ignored.
func (s *Server) ModRequest(ctx context.Context, id string, conn *modproto.Conn, m *modproto.Message) {
	if m == nil || m.ID == nil {
		return
	}
	reqID, method, params := *m.ID, m.Method, m.Params
	slot, ok := s.modAdmit(id)
	if !ok {
		_ = conn.ReplyError(reqID, modproto.CodeFailed,
			fmt.Sprintf("%s: too many calls in flight (%d); wait for their answers", method, modCallsAtOnce+modCallsWaiting))
		return
	}
	go func() {
		defer s.modDone(id)
		select {
		case slot <- struct{}{}:
			defer func() { <-slot }()
		case <-ctx.Done():
			_ = conn.ReplyError(reqID, modproto.CodeFailed, method+": "+ctx.Err().Error())
			return
		}
		res, err := s.CallMod(ctx, id, method, params)
		if err == nil {
			err = conn.Reply(reqID, res)
			if err != nil {
				s.log.Printf("mods: %s: reply to %s: %v", id, method, err)
			}
			return
		}
		code := modproto.CodeFailed
		if e, ok := err.(*modbroker.Error); ok {
			code = e.Code
		}
		_ = conn.ReplyError(reqID, code, err.Error())
	}()
}

// modAdmit counts a call of mod id in, and returns the mod's semaphore of
// modCallsAtOnce slots; false when the mod already has
// modCallsAtOnce+modCallsWaiting calls in flight.
func (s *Server) modAdmit(id string) (chan struct{}, bool) {
	mb := &s.modBr
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if mb.inFlight[id] >= modCallsAtOnce+modCallsWaiting {
		return nil, false
	}
	if mb.busy == nil {
		mb.busy, mb.inFlight = map[string]chan struct{}{}, map[string]int{}
	}
	if mb.busy[id] == nil {
		mb.busy[id] = make(chan struct{}, modCallsAtOnce)
	}
	mb.inFlight[id]++
	return mb.busy[id], true
}

// modDone counts a call of mod id out (modAdmit).
func (s *Server) modDone(id string) {
	mb := &s.modBr
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if mb.inFlight[id]--; mb.inFlight[id] <= 0 {
		delete(mb.inFlight, id)
	}
}

func (s *Server) broker() *modbroker.Broker {
	mb := &s.modBr
	mb.once.Do(func() {
		h, _ := os.UserHomeDir()
		tm, _ := home.Dir()
		state, _ := home.ModStateDir()
		s.mu.Lock()
		reg := s.agents
		s.mu.Unlock()
		mb.b = modbroker.New(modbroker.Options{
			Mod: s.modNamed, Home: h, TMHome: tm, StateDir: state,
			Secrets: agent.Secrets(reg, h, os.Getenv), Audit: s.modLog,
		})
		s.modMethods(mb.b)
	})
	return mb.b
}

// modNamed is installed mod id as the registry has it (modRegistry).
func (s *Server) modNamed(id string) (mod.Mod, bool) {
	r := s.modRegistry()
	if r == nil {
		return mod.Mod{}, false
	}
	return r.Get(id)
}

// modRegistry is the installed mods (installedMods), their files looked at again
// when older than modRegistryAge; nil when they can't be read.
func (s *Server) modRegistry() *mod.Registry {
	reg, _, _ := s.installedMods(modRegistryAge)
	return reg
}

// installedMods is the installed mods, whether config.toml loaded, and their
// files' stamp (modsStamp). It is the one reader of the mods for the
// hosts (runModHosts), the broker (modRegistry) and the guard
// (hookRules): it looks at the stamp when the last look is older than
// maxAge, and reads config.toml and the mods anew only when the stamp
// changed. reg is nil when they can't be read.
func (s *Server) installedMods(maxAge time.Duration) (reg *mod.Registry, cfgOK bool, stamp string) {
	mb := &s.modBr
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if mb.reg != nil && time.Since(mb.checked) <= maxAge {
		return mb.reg, mb.cfgOK, mb.stamp
	}
	// Stamped before the read: a change in between reads again next time.
	stamp = modsStamp()
	if mb.reg != nil && stamp == mb.stamp {
		mb.checked = time.Now()
		return mb.reg, mb.cfgOK, mb.stamp
	}
	c, err := config.Load()
	if err != nil && c == nil {
		s.log.Printf("mods: config.toml: %v", err)
	}
	cfgOK = err == nil
	r, err := mod.Load(c)
	if err != nil {
		s.log.Printf("mods: %v", err)
		return nil, false, stamp
	}
	mb.reg, mb.cfgOK, mb.stamp, mb.checked = r, cfgOK, stamp, time.Now()
	return r, cfgOK, stamp
}

// forgetMods has the mods read anew at their next use, whatever their
// stamp says.
func (s *Server) forgetMods() {
	mb := &s.modBr
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.reg = nil
}

// modsStamp says, cheaply (stats only), whether the installed mods may
// have changed: config.toml (which mods are enabled and for which
// projects, their grants and settings) and each installed mod's mod.toml
// (an install, update or removal).
func modsStamp() string {
	var b strings.Builder
	if path, err := config.Path(); err == nil {
		stampFile(&b, path)
	}
	if mods, err := home.ModsDir(); err == nil {
		ents, _ := os.ReadDir(mods)
		for _, e := range ents {
			stampFile(&b, filepath.Join(mods, e.Name(), "mod.toml"))
		}
	}
	return b.String()
}

// stampFile adds file path's size and modification time to b.
func stampFile(b *strings.Builder, path string) {
	if fi, err := os.Stat(path); err == nil {
		fmt.Fprintf(b, "%s %d %d;", path, fi.Size(), fi.ModTime().UnixNano())
	} else {
		fmt.Fprintf(b, "%s -;", path)
	}
}

// modLog appends line to mod id's log, ~/.terminatr/logs/mods/<id>.log,
// as one write in append mode beside the supervisor's lines; the
// supervisor rotates the file (modsup.LogMax).
func (s *Server) modLog(id, line string) {
	dir, err := home.ModLogsDir()
	if err != nil || mod.CheckID(id) != nil {
		return
	}
	s.modBr.logMu.Lock()
	defer s.modBr.logMu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.log.Printf("mods: %v", err)
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, id+".log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		s.log.Printf("mods: %v", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02T15:04:05.000Z07:00"), line)
}

// modProject reads the project params of a call, the project's slug as
// "slug" (project.get, tasks.list) or "project", and opens the project,
// which the mod must run for.
func modProject(c *modbroker.Call, args any) (*project.Project, error) {
	if err := c.Decode(args); err != nil {
		return nil, err
	}
	var a struct {
		Slug    string `json:"slug"`
		Project string `json:"project"`
	}
	_ = json.Unmarshal(orEmpty(c.Params), &a)
	slug := cmp.Or(a.Slug, a.Project)
	if err := c.Project(slug); err != nil {
		return nil, err
	}
	p, err := project.Open(slug)
	if err != nil {
		return nil, modbroker.NotFound("no project %s", slug)
	}
	return p, nil
}

func orEmpty(p json.RawMessage) json.RawMessage {
	if len(p) == 0 {
		return json.RawMessage("{}")
	}
	return p
}

// The answers' shapes are mod API v1's (the mods design §5; the schema
// the $ bindings are built from): a project by its slug, and a task of a
// board.
type (
	ModProject struct {
		Slug string `json:"slug"`
	}
	ModTask = modapi.Task
)

// modMethods adds the server's methods to b.
func (s *Server) modMethods(b *modbroker.Broker) {
	b.Handle("projects.list", mod.PermProjectRead, func(_ context.Context, c *modbroker.Call) (any, error) {
		list, err := project.List()
		if err != nil {
			return nil, err
		}
		out := []ModProject{}
		for _, sum := range list {
			if sum.Error == "" && c.Mod.Runs(sum.Slug) {
				out = append(out, ModProject{Slug: sum.Slug})
			}
		}
		return out, nil
	})
	b.Handle("project.get", mod.PermProjectRead, func(_ context.Context, c *modbroker.Call) (any, error) {
		p, err := modProject(c, &struct{}{})
		if err != nil {
			return nil, err
		}
		return s.projectWatchState(p.Slug)
	})
	b.Handle("tasks.list", mod.PermProjectRead, func(_ context.Context, c *modbroker.Call) (any, error) {
		p, err := modProject(c, &struct{}{})
		if err != nil {
			return nil, err
		}
		board, err := p.Tasks().Load()
		if err != nil {
			return nil, err
		}
		items, _ := p.Inbox()
		return modTasks(board, items), nil
	})
	b.Handle("thread.get", mod.PermProjectRead, func(_ context.Context, c *modbroker.Call) (any, error) {
		var a struct {
			Thread string `json:"thread"`
		}
		p, err := modProject(c, &a)
		if err != nil {
			return nil, err
		}
		// An unresolved thread as the project's watch shows it; null
		// for none.
		w, err := s.projectWatchState(p.Slug)
		if err != nil {
			return nil, err
		}
		for _, t := range w.Threads {
			if t.ID == a.Thread {
				return t, nil
			}
		}
		return nil, nil
	})
	b.Handle("report.get", mod.PermProjectRead, func(_ context.Context, c *modbroker.Call) (any, error) {
		var a struct {
			Thread string `json:"thread"`
		}
		p, err := modProject(c, &a)
		if err != nil {
			return nil, err
		}
		if r := modReport(p.Slug, a.Thread); r != nil {
			return r, nil
		}
		return nil, nil
	})
	b.Handle("thread.changes", mod.PermProjectRead, s.modThreadChanges)
	b.Handle("memory.list", mod.PermProjectRead, func(_ context.Context, c *modbroker.Call) (any, error) {
		p, err := modProject(c, &struct{}{})
		if err != nil {
			return nil, err
		}
		files, err := p.MemoryFiles()
		if files == nil {
			files = []project.MemoryFile{}
		}
		return files, err
	})
	b.Handle("memory.read", mod.PermProjectRead, func(_ context.Context, c *modbroker.Call) (any, error) {
		var a struct {
			Path string `json:"path"`
		}
		p, err := modProject(c, &a)
		if err != nil {
			return nil, err
		}
		text, err := p.ReadMemoryFile(a.Path)
		if errors.Is(err, project.ErrNotMemory) || errors.Is(err, fs.ErrNotExist) {
			return nil, modbroker.NotFound("%s has no memory file %q", p.Slug, a.Path)
		}
		return text, err
	})
	b.Handle("inbox.raise", mod.PermInbox, s.modRaise)
	s.modActionMethods(b)
	b.Handle("ui.view.set", mod.PermViews, s.modViewSet)
	b.Handle("ui.view.open", mod.PermViews, s.modViewOpen)
	b.Handle("ui.cell.set", "", s.modCellSet)
	b.Handle("ui.status.set", "", s.modStatusSet)
	b.Handle("ui.toast", "", s.modToast)
	b.Handle("ui.notify", mod.PermNotify, s.modNotify)
	b.Handle("annotate", mod.PermProjectRead, s.modAnnotate)
}

// annotate {target: {project, task | thread}, key, spans}: shows spans as
// a badge by the task or thread, in the mod's own annotation store (never
// TASKS.md); null or no spans removes it. The target must exist and the
// mod must run for its project. Journaled as mod:<id> when it changes
// something.
func (s *Server) modAnnotate(_ context.Context, c *modbroker.Call) (any, error) {
	var a struct {
		Target struct {
			Project string `json:"project"`
			Task    string `json:"task"`
			Thread  string `json:"thread"`
		} `json:"target"`
		Key   string          `json:"key"`
		Spans []modannot.Span `json:"spans"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	if err := c.Project(a.Target.Project); err != nil {
		return nil, err
	}
	if (a.Target.Task == "") == (a.Target.Thread == "") {
		return nil, modbroker.BadParams("annotate: the target is a task or a thread")
	}
	if err := modannot.CheckKey(a.Key); err != nil {
		return nil, modbroker.BadParams("annotate: %v", err)
	}
	spans, err := modannot.Clean(a.Spans)
	if err != nil {
		return nil, modbroker.BadParams("annotate: %v", err)
	}
	p, err := project.Open(a.Target.Project)
	if err != nil {
		return nil, modbroker.NotFound("no project %s", a.Target.Project)
	}
	var target string
	if a.Target.Task != "" {
		n, ok := tasks.ParseRef(a.Target.Task)
		if !ok {
			return nil, modbroker.BadParams("annotate: %q is not a task id", a.Target.Task)
		}
		b, err := p.Tasks().Load()
		if err != nil {
			return nil, err
		}
		t := b.Find(n)
		if t == nil {
			return nil, modbroker.NotFound("no task %s in %s", a.Target.Task, p.Slug)
		}
		target = modannot.Task(t.Ref())
	} else {
		recs, err := thread.List(p)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(recs, func(r *thread.Record) bool { return r.ID == a.Target.Thread }) {
			return nil, modbroker.NotFound("no thread %s in %s", a.Target.Thread, p.Slug)
		}
		target = modannot.Thread(a.Target.Thread)
	}
	changed, err := modannot.Set(s.broker().OwnDir(c.Mod.ID), p.Slug, target, a.Key, spans)
	if err != nil {
		return nil, modbroker.Denied("annotate: %v", err)
	}
	if changed {
		if err := p.Journal(c.Caller, "annotate", target, a.Key); err != nil {
			s.log.Printf("mods: %s: journal: %v", p.Slug, err)
		}
	}
	return nil, nil
}

// inbox.raise {project, item: {subject, summary}} (or the two flat): an
// item of kind mod for the
// coordinator, its subject led by the mod's id. Data only: a nudge names
// no mod's words (ticker.NudgeText). Journaled as mod:<id>.
func (s *Server) modRaise(_ context.Context, c *modbroker.Call) (any, error) {
	type text struct {
		Subject string `json:"subject"`
		Summary string `json:"summary"`
	}
	var a struct {
		text
		Item *text `json:"item"`
	}
	p, err := modProject(c, &a)
	if err != nil {
		return nil, err
	}
	if a.Item != nil {
		a.text = *a.Item
	}
	subject, summary := textsafe.Line(a.Subject, modSubjectMax), textsafe.Line(a.Summary, modSummaryMax)
	if subject == "" {
		return nil, modbroker.BadParams("inbox.raise: no subject")
	}
	lead := c.Caller.String() + " "
	items, err := p.Inbox()
	if err != nil {
		return nil, err
	}
	n := 0
	for _, it := range items {
		if it.Kind == project.KindMod && strings.HasPrefix(it.Subject, lead) {
			n++
		}
	}
	if n >= modItemsUnread {
		return nil, modbroker.Denied("%s has %d unhandled items in %s; it raises more once the coordinator handles them", c.Mod.ID, n, p.Slug)
	}
	it, err := p.AddItem(project.KindMod, lead+subject, summary, false)
	if err != nil {
		return nil, err
	}
	if err := p.Journal(c.Caller, "inbox.raise", it.ID, ""); err != nil {
		s.log.Printf("mods: %s: journal: %v", p.Slug, err)
	}
	s.modEv.wake()
	return map[string]string{"id": it.ID}, nil
}

// modTasks is $.tasks.list's answer: board's tasks, in board order, with
// what the unhandled items ask about each.
func modTasks(board *tasks.Board, items []project.Item) []ModTask {
	out := []ModTask{}
	for _, t := range board.Tasks {
		steps := make([]modapi.TaskStep, 0, len(t.Steps))
		for _, st := range t.Steps {
			steps = append(steps, modapi.TaskStep{Text: st.Text, Done: st.Done})
		}
		out = append(out, ModTask{ID: t.Ref(), Title: t.Title, Status: string(t.Status),
			StepsDone: t.StepsDone(), StepsTotal: len(t.Steps), Thread: t.Thread,
			Steps: steps, Updated: t.Updated, Asked: project.TaskAsked(items, t.Ref())})
	}
	return out
}
