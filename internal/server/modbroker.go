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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/project"
)

// modRegistryAge is how long the broker trusts the registry it read: a
// grant the human takes back applies within it.
const modRegistryAge = time.Second

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

	mu  sync.Mutex
	reg *mod.Registry
	at  time.Time
	// logMu serialises writes to the mods' logs.
	logMu sync.Mutex
}

// CallMod answers mod id's call of method from its host (the supervisor
// passes each $ request here). The result is sent to the mod as JSON; an
// error is a *modbroker.Error with its code.
func (s *Server) CallMod(ctx context.Context, id, method string, params json.RawMessage) (any, error) {
	return s.broker().Call(ctx, id, method, params)
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

// modNamed is installed mod id as the registry has it, read anew when
// older than modRegistryAge.
func (s *Server) modNamed(id string) (mod.Mod, bool) {
	mb := &s.modBr
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if mb.reg == nil || time.Since(mb.at) > modRegistryAge {
		c, err := config.Load()
		if err != nil && c == nil {
			s.log.Printf("mods: config.toml: %v", err)
		}
		r, err := mod.Load(c)
		if err != nil {
			s.log.Printf("mods: %v", err)
			return mod.Mod{}, false
		}
		mb.reg, mb.at = r, time.Now()
	}
	return mb.reg.Get(id)
}

// modLog appends line to mod id's log, ~/.terminatr/logs/mods/<id>.log.
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
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
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
	ModTask struct {
		ID         string `json:"id"` // "T12"
		Title      string `json:"title"`
		Status     string `json:"status"`
		StepsDone  int    `json:"steps_done"`
		StepsTotal int    `json:"steps_total"`
		Thread     string `json:"thread,omitempty"`
	}
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
		out := []ModTask{}
		board, err := p.Tasks().Load()
		if err != nil {
			return nil, err
		}
		for _, t := range board.Tasks {
			out = append(out, ModTask{ID: t.Ref(), Title: t.Title, Status: string(t.Status),
				StepsDone: t.StepsDone(), StepsTotal: len(t.Steps), Thread: t.Thread})
		}
		return out, nil
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
	b.Handle("inbox.raise", mod.PermInbox, s.modRaise)
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
	subject, summary := modLine(a.Subject, modSubjectMax), modLine(a.Summary, modSummaryMax)
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

// modLine is a mod's text as one line of at most n runes, its control
// characters (escape sequences included) made spaces.
func modLine(s string, n int) string {
	return oneLine(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return ' '
		}
		return r
	}, s), n)
}
