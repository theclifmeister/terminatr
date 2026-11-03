// Package program reads and makes program folders (the programs design
// §3.1): ~/.terminatr/projects/_programs/<slug>/, a program's PROGRAM.md,
// CONTEXT.md, memory, journal, inbox, questions, messages, designs,
// uploads and its small board of umbrella tasks. A program has no repo,
// no worktrees and no threads. Its folder parts are a project.Folder's,
// so the journal, inbox, questions, memory and designs helpers are the
// projects' own.
//
// Membership and authority are config.toml's ([programs.<slug>]), the
// user's file: this package reads them and never writes them. Nothing
// here checks [features] programs: the entry points that act on a
// program (the CLI, the server's program.* methods) refuse programs-off
// first.
package program

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
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// File is the program's own file, as PROJECT.md is a project's.
const File = "PROGRAM.md"

// Meta is PROGRAM.md's front matter.
type Meta struct {
	Goal    string    `toml:"goal" yaml:"goal"`
	Created time.Time `toml:"created" yaml:"created"`
}

// Program is one program folder.
type Program struct {
	project.Folder
	Meta Meta
	// Instructions is PROGRAM.md's body: standing instructions every
	// child coordinator is pointed at.
	Instructions string
	// Members are config.toml's [programs.<slug>] projects, in order: a
	// project's slug, or <alias>/<slug> on a paired server. None when
	// the file has no table for it.
	Members []string
	// LeftOut is set when a project here has the program's slug: the
	// project wins, and the program's director isn't started and nothing
	// routes to it until one is renamed (§3.4).
	LeftOut bool
	// Paired reaches members' questions on paired servers through tm's
	// server (merges and answers, §7.2); nil refuses them.
	Paired Paired
}

// Error is a refusal with a stable code.
type Error = tasks.Error

func refuse(code, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

var now = time.Now

// Dir returns the folder of program slug, under home.ProgramsDir.
func Dir(slug string) (string, error) {
	if !config.ValidProgramSlug(slug) {
		return "", refuse("invalid-program", "%q is not a program slug (a-z, 0-9 and -, lower case)", slug)
	}
	d, err := home.ProgramsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, slug), nil
}

// Open reads program slug: its PROGRAM.md, and its members from cfg
// (config.Load's; nil reads it).
func Open(slug string, cfg *config.Config) (*Program, error) {
	dir, err := Dir(slug)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg, _ = config.Load() // a safety error still gives the tables
	}
	p := &Program{Folder: project.Folder{Slug: slug, Dir: dir, Kind: project.KindProgram}}
	data, err := os.ReadFile(p.Path(File))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, refuse("unknown-program", "no program %q", slug)
	}
	if err != nil {
		return nil, err
	}
	body, err := mdfile.Decode(data, &p.Meta)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Path(File), err)
	}
	p.Instructions = strings.TrimSpace(string(body))
	if t, ok := cfg.Program(slug); ok {
		p.Members = t.Projects
	}
	p.LeftOut = project.ProgramLeftOut(slug)
	return p, nil
}

// Tasks is the program's board: umbrella and sweep tasks only (§5.3),
// journaled into the program's journal. It has no repos.
func (p *Program) Tasks() *tasks.Store {
	return &tasks.Store{Dir: p.Dir, Slug: p.Slug, LinkSlug: p.LinkSlug(), Now: func() time.Time { return now() }, Events: events{&p.Folder}, Program: true}
}

type events struct{ f *project.Folder }

func (e events) Journal(c caller.Caller, action, ref, detail string) error {
	return e.f.Journal(c, action, ref, detail)
}

// Owned is what in the program folder only tm writes: the folder parts
// every Folder has (project.Folder.SharedOwned), PROGRAM.md's lock and
// the board's files. The director edits CONTEXT.md, MEMORY.md and
// memory/ (PROGRAM.md's body only on the user's word) and leaves these
// to tm.
func (p *Program) Owned() (dirs, files []string) {
	dirs, files = p.SharedOwned()
	files = append(files, mdfile.LockPath(p.Path(File)), p.Path(DepsFile), p.Path(RecommendFile), mdfile.LockPath(p.Path(RecommendFile)))
	td, tf := tasks.Owned(p.Dir)
	return append(dirs, td...), append(files, tf...)
}

// Options for New.
type Options struct {
	Slug string
	Goal string
	Now  time.Time
	// Taken says why a name can't be a program's beyond this package's
	// checks ("" when it can): a paired server's alias or name here, or
	// this server's own, which only the caller knows (§3.4).
	Taken func(slug string) string
}

// New makes a program folder. It refuses a slug that isn't one
// (invalid-program), that a project has (slug-taken: the one namespace,
// §3.4) and that a program already has (program-exists). It writes
// nothing to config.toml: membership is the user's, set in the TUI.
// A paired server's name is checked through o.Taken, which the caller
// gives: it knows them.
func New(o Options) (*Program, error) {
	slug := strings.TrimSpace(o.Slug)
	dir, err := Dir(slug)
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(o.Goal, "\r\n") {
		return nil, refuse("invalid-goal", "the goal must be one line")
	}
	if pd, err := project.Dir(slug); err == nil {
		if _, err := os.Stat(pd); err == nil {
			return nil, refuse("slug-taken", "%s is a project", slug)
		}
	}
	if o.Taken != nil {
		if why := o.Taken(slug); why != "" {
			return nil, refuse("slug-taken", "%s: %s", slug, why)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, refuse("program-exists", "program %q already exists at %s", slug, dir)
		}
		return nil, err
	}
	at := o.Now
	if at.IsZero() {
		at = now()
	}
	p := &Program{
		Folder:       project.Folder{Slug: slug, Dir: dir, Kind: project.KindProgram},
		Meta:         Meta{Goal: strings.TrimSpace(o.Goal), Created: at.UTC().Truncate(time.Second)},
		Instructions: strings.TrimSpace(instructionsTemplate),
	}
	if err := p.scaffold(); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	cfg, _ := config.Load()
	if t, ok := cfg.Program(slug); ok {
		p.Members = t.Projects
	}
	return p, nil
}

func (p *Program) scaffold() error {
	for _, d := range []string{"memory", "inbox/done", "messages", "designs", "uploads"} {
		if err := os.MkdirAll(p.Path(d), 0o755); err != nil {
			return err
		}
	}
	front, err := mdfile.Join(p.Meta, []byte(instructionsTemplate))
	if err != nil {
		return err
	}
	files := map[string][]byte{
		File:         front,
		"CONTEXT.md": []byte(contextTemplate),
		"MEMORY.md":  []byte(memoryTemplate),
		"JOURNAL.md": []byte("# Journal\n\n"),
	}
	for name, body := range files {
		// A fresh folder has no other writers yet: no lock needed.
		if err := fsx.WriteAtomic(p.Path(name), body, 0o644); err != nil {
			return err
		}
	}
	if err := tasks.Scaffold(p.Dir); err != nil {
		return err
	}
	return p.Journal(caller.Caller{Kind: caller.Human}, "program.new", p.Slug, "")
}

// Summary is one program in a listing.
type Summary struct {
	Slug    string   `json:"slug"`
	Goal    string   `json:"goal,omitempty"`
	Dir     string   `json:"dir"`
	Members []string `json:"members,omitempty"`
	// Folder is false while it has no PROGRAM.md: a [programs.<slug>]
	// table with no folder yet, or a bare folder.
	Folder  bool   `json:"folder"`
	LeftOut bool   `json:"left_out,omitempty"`
	Error   string `json:"error,omitempty"`
}

// List returns every program here (project.ProgramSlugs: the tables and
// the folders), by slug. One that can't be read is listed with its error.
func List(cfg *config.Config) ([]Summary, error) {
	if cfg == nil {
		cfg, _ = config.Load()
	}
	var out []Summary
	for _, slug := range project.ProgramSlugs(cfg) {
		dir, err := Dir(slug)
		if err != nil {
			continue
		}
		s := Summary{Slug: slug, Dir: dir, LeftOut: project.ProgramLeftOut(slug)}
		if t, ok := cfg.Program(slug); ok {
			s.Members = t.Projects
		}
		p, err := Open(slug, cfg)
		var e *Error
		switch {
		case errors.As(err, &e) && e.Code == "unknown-program":
		case err != nil:
			s.Folder, s.Error = true, err.Error()
		default:
			s.Folder, s.Goal = true, p.Meta.Goal
		}
		out = append(out, s)
	}
	return out, nil
}

// Delete is tm program delete, the human's (§3.4): the program's folder
// moves to <home>/.trash/_programs-<slug>-<UTC time>, and its
// [programs.<slug>] table leaves config.toml, its members' membership
// with it. The children are left alone. It returns where the folder
// went ("" when there was none: only a table). It works whatever
// [features] programs says, so dormant programs can be cleared.
func Delete(c caller.Caller, slug string, at time.Time) (string, error) {
	dir, err := Dir(slug)
	if err != nil {
		return "", err
	}
	cfg, _ := config.Load()
	_, table := cfg.Program(slug)
	_, statErr := os.Stat(dir)
	if !table && statErr != nil {
		return "", refuse("unknown-program", "no program %q", slug)
	}
	dst := ""
	if statErr == nil {
		trash, err := home.TrashDir()
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(trash, 0o700); err != nil {
			return "", err
		}
		base := filepath.Join(trash, config.ProgramsDirName+"-"+slug+"-"+at.UTC().Format("20060102T150405Z"))
		dst = base
		for i := 2; ; i++ {
			if _, err := os.Lstat(dst); os.IsNotExist(err) {
				break
			}
			dst = fmt.Sprintf("%s-%d", base, i)
		}
		// The journal line goes in first, so it travels with the folder.
		f := project.Folder{Slug: slug, Dir: dir, Kind: project.KindProgram}
		_ = f.Journal(c, "program.delete", slug, "moved to "+dst)
		if err := os.Rename(dir, dst); err != nil {
			return "", err
		}
	}
	if table {
		if err := config.RemoveProgram(slug); err != nil {
			if dst != "" {
				return dst, fmt.Errorf("moved to %s, but its [programs.%s] table stays: %w", dst, slug, err)
			}
			return "", err
		}
	}
	return dst, nil
}
