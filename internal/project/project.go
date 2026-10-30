package project

import (
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// Meta is PROJECT.md's front matter. A project has no display name: its
// slug is its only name. A name key left by an older tm is ignored, and
// dropped the next time tm writes the front matter; so is a repos key,
// the repo list's place before config.toml (Project.Repos, MoveRepos).
type Meta struct {
	Goal    string    `toml:"goal" yaml:"goal"`
	Created time.Time `toml:"created" yaml:"created"`
	// CodeHost and AzureURL override where the repos' pull requests live
	// (codehost.Config): "github", "azure", or the kind a code host mod
	// serves ("gitlab"), and the Azure DevOps
	// organization URL for a server tm doesn't recognize. Unset, each
	// repo's origin URL decides.
	CodeHost string `toml:"code_host,omitempty" yaml:"code_host,omitempty"`
	AzureURL string `toml:"azure_url,omitempty" yaml:"azure_url,omitempty"`
}

// CodeHost is the project's code host override, for codehost.Pick.
func (p *Project) CodeHost() codehost.Config {
	c := codehost.Config{CodeHost: p.Meta.CodeHost, AzureURL: p.Meta.AzureURL, Project: p.Slug}
	if c.AzureURL != "" {
		c.OriginRepos = p.originRepos()
	}
	return c
}

// originRepos are the repos whose origin remote may vouch for azure_url's
// host: the ones in config.toml, which only the user writes, less any
// that a live thread works in the main checkout of (an adopted thread or
// a checkout one): its agent can edit that repo's .git/config.
func (p *Project) originRepos() []string {
	busy := map[string]bool{}
	ents, _ := os.ReadDir(filepath.Join(p.Dir, "threads"))
	for _, e := range ents {
		var r struct {
			Repo     string `toml:"repo"`
			State    string `toml:"state"`
			Adopted  bool   `toml:"adopted"`
			Checkout bool   `toml:"checkout"`
		}
		b, err := os.ReadFile(filepath.Join(p.Dir, "threads", e.Name(), "thread.toml"))
		if err != nil {
			continue
		}
		if toml.Unmarshal(b, &r) != nil || r.State == "resolved" {
			continue
		}
		if r.Adopted || r.Checkout {
			busy[filepath.Clean(r.Repo)] = true
		}
	}
	var out []string
	for _, r := range p.Repos {
		if !busy[filepath.Clean(r)] {
			out = append(out, r)
		}
	}
	return out
}

// Project is one project folder, ~/.terminatr/projects/<slug>/.
type Project struct {
	Slug string
	Dir  string
	Meta Meta
	// Repos are the project's repositories, those the user approved for
	// its threads' worktrees: [projects.<slug>] repos in config.toml,
	// the human's file, never PROJECT.md, which the coordinator writes
	// (T335). Only the user adds one (SetRepo).
	Repos []string
	// Instructions is PROJECT.md's body: the standing instructions.
	Instructions string
}

// Error is a refusal with a stable code, like tasks.Error.
type Error = tasks.Error

func refuse(code, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidSlug reports whether s can name a project folder.
func ValidSlug(s string) bool { return len(s) <= 64 && slugRE.MatchString(s) }

// Slugify turns text into a slug: lower case, runs of other characters
// become one '-'. It suggests a slug for text that isn't one.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "-")
	}
	return s
}

// SlugFor turns what the user typed as a project name into its slug
// (Demo App → demo-app). It refuses text with no letter or digit.
func SlugFor(input string) (string, error) {
	s := strings.TrimSpace(input)
	if ValidSlug(s) {
		return s, nil
	}
	if s = Slugify(s); s == "" {
		return "", refuse("invalid-project", "%q gives no project slug (it needs a letter or digit)", input)
	}
	return s, nil
}

// invalidSlug refuses s as a slug, suggesting one when s has letters or
// digits (an upper-case "Demo App" suggests demo-app).
func invalidSlug(s string) error {
	if sug := Slugify(s); sug != "" && sug != s {
		return refuse("invalid-project", "%q is not a project slug (a-z, 0-9 and -, lower case): try %s", s, sug)
	}
	return refuse("invalid-project", "%q is not a project slug (a-z, 0-9 and -, lower case)", s)
}

// Dir returns the folder of a project slug.
func Dir(slug string) (string, error) {
	if !ValidSlug(slug) {
		return "", invalidSlug(slug)
	}
	d, err := home.ProjectsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, slug), nil
}

// PeerNameTaken says why name can't be a paired server's display name
// here (docs/SPEC.md §16.8): it is this server's own name self, or a
// project here, and <name>/<project> would read as either. "" when it
// can be.
func PeerNameTaken(name, self string) string {
	if name == self {
		return "it is this server's own name"
	}
	if dir, err := Dir(name); err == nil {
		if _, err := os.Stat(dir); err == nil {
			return "a project here is called that"
		}
	}
	return ""
}

// Open reads a project by slug.
func Open(slug string) (*Project, error) {
	cfg, _ := config.Load() // a safety error still gives the tables
	return OpenWith(slug, cfg)
}

// OpenWith reads a project by slug, its repos from cfg, the config
// config.Load gave (which a reader of many projects loads once).
func OpenWith(slug string, cfg *config.Config) (*Project, error) {
	dir, err := Dir(slug)
	if err != nil {
		return nil, err
	}
	p := &Project{Slug: slug, Dir: dir}
	data, err := os.ReadFile(p.Path("PROJECT.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, refuse("unknown-project", "no project %q (tm project list)", slug)
	}
	if err != nil {
		return nil, err
	}
	body, err := mdfile.Decode(data, &p.Meta)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Path("PROJECT.md"), err)
	}
	p.Instructions = strings.TrimSpace(string(body))
	p.Repos = approvedRepos(cfg, slug, data)
	return p, nil
}

// approvedRepos is project slug's repo list from config.toml (cfg), or
// none when the file doesn't parse (nil). Until MoveRepos has run, a
// project the file has no list for keeps PROJECT.md's (data), as an
// older tm did.
func approvedRepos(cfg *config.Config, slug string, data []byte) []string {
	if repos, set := cfg.Repos(slug); set || reposMoved() {
		return repos
	}
	return legacyRepos(data)
}

// legacyRepos is the repos key of a PROJECT.md (data) an older tm wrote.
func legacyRepos(data []byte) []string {
	var old struct {
		Repos []string `toml:"repos" yaml:"repos"`
	}
	if _, err := mdfile.Decode(data, &old); err != nil {
		return nil
	}
	var out []string
	for _, r := range old.Repos {
		if filepath.IsAbs(r) {
			out = append(out, filepath.Clean(r))
		}
	}
	return out
}

// Path joins a name onto the project folder.
func (p *Project) Path(elem ...string) string {
	return filepath.Join(append([]string{p.Dir}, elem...)...)
}

// Tasks returns the task store for this project, journaling into it and
// raising confirmations in its inbox.
func (p *Project) Tasks() *tasks.Store {
	return &tasks.Store{Dir: p.Dir, Slug: p.Slug, Now: func() time.Time { return now() }, Events: events{p}, Repos: p.Repos}
}

// Options for New.
type Options struct {
	Slug  string // the project's only name, lower case (ValidSlug)
	Goal  string
	Repos []string // absolute or relative paths of existing directories
	Now   time.Time
}

// New creates a project folder with the layout of §5.1. It refuses with
// project-exists if the slug is taken.
func New(o Options) (*Project, error) {
	slug, err := SlugFor(o.Slug)
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(o.Goal, "\r\n") {
		return nil, refuse("invalid-goal", "the goal must be one line")
	}
	var repos []string
	for _, r := range o.Repos {
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, err
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return nil, refuse("invalid-repo", "%s is not a directory", abs)
		}
		repos = append(repos, abs)
	}
	dir, err := Dir(slug)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, refuse("project-exists", "project %q already exists at %s", slug, dir)
		}
		return nil, err
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	p := &Project{Slug: slug, Dir: dir, Repos: repos, Meta: Meta{Goal: strings.TrimSpace(o.Goal), Created: now.UTC().Truncate(time.Second)}}
	p.Instructions = strings.TrimSpace(instructionsTemplate)
	if err := p.scaffold(); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	// The list goes to config.toml, replacing one a table of the slug
	// still had: a new project starts with only the repos named here.
	cfg, _ := config.Load()
	if _, set := cfg.Repos(slug); set || len(repos) > 0 {
		if err := config.SetRepos(slug, repos); err != nil {
			return p, fmt.Errorf("created %s, but its repos aren't saved in config.toml: %w", dir, err)
		}
	}
	return p, nil
}

// Owned is what in the project folder only tm writes (docs/SPEC.md
// §5.2), as absolute paths: folders and single files, existing or not,
// with the lock files tm takes on them. The coordinator, whose folder it
// is, edits CONTEXT.md, MEMORY.md, memory/ and PROJECT.md and leaves
// these to tm, which writes and appends to them outside its sandbox.
func (p *Project) Owned() (dirs, files []string) {
	for _, d := range []string{"threads", "inbox", "journal", "messages", "uploads"} {
		dirs = append(dirs, p.Path(d))
	}
	for _, f := range []string{"JOURNAL.md", questionsFile, designsFile, "AGENTS.md", "CLAUDE.md"} {
		files = append(files, p.Path(f))
	}
	for _, f := range []string{"JOURNAL.md", questionsFile, designsFile, "PROJECT.md"} {
		files = append(files, mdfile.LockPath(p.Path(f)))
	}
	// The board's (the tasks package says where it is kept).
	td, tf := tasks.Owned(p.Dir)
	return append(dirs, td...), append(files, tf...)
}

func (p *Project) scaffold() error {
	for _, d := range []string{"memory", "inbox/done", "threads", "uploads"} {
		if err := os.MkdirAll(p.Path(d), 0o755); err != nil {
			return err
		}
	}
	project, err := mdfile.Join(p.Meta, []byte(instructionsTemplate))
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(p.Path("PROJECT.md"), project, 0o644); err != nil {
		return err
	}
	if err := p.WriteRoleFile(); err != nil {
		return err
	}
	files := map[string]string{
		"CONTEXT.md": contextTemplate,
		"MEMORY.md":  memoryTemplate,
		"JOURNAL.md": "# Journal\n\n",
	}
	for name, body := range files {
		// A fresh folder has no other writers yet: no lock needed.
		if err := fsx.WriteAtomic(p.Path(name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := tasks.Scaffold(p.Dir); err != nil {
		return err
	}
	return p.Journal(caller.Caller{Kind: caller.Human}, "project.new", p.Slug, "")
}

// WriteRoleFile (re)generates AGENTS.md, the coordinator's role file, and
// links the agents' role_files (CLAUDE.md) to it (§5.2, §7.8).
func (p *Project) WriteRoleFile() error {
	if err := fsx.WriteAtomic(p.Path("AGENTS.md"), []byte(roleFile(p)), 0o644); err != nil {
		return err
	}
	dir, _ := home.AgentsDir()
	reg, _ := agent.Load(dir) // a broken user manifest is skipped
	for _, name := range agent.RoleFiles(reg) {
		if name == "AGENTS.md" || name != filepath.Base(name) {
			continue
		}
		if err := fsx.LinkRoleFile(p.Dir, name, "AGENTS.md"); err != nil {
			return err
		}
	}
	return nil
}

// RefreshRoleFile rewrites AGENTS.md when tm generated it and its text is
// out of date (an older tm, a rename), so a coordinator starts from the
// current one (T109). A file without tm's marker is the user's: kept.
func (p *Project) RefreshRoleFile() error {
	old, err := os.ReadFile(p.Path("AGENTS.md"))
	if err != nil || !strings.Contains(string(old), roleFileMarker) {
		return nil
	}
	if text := roleFile(p); string(old) != text {
		return fsx.WriteAtomic(p.Path("AGENTS.md"), []byte(text), 0o644)
	}
	return nil
}

// Summary is one row of `tm project list`.
type Summary struct {
	Slug   string         `json:"slug"`
	Goal   string         `json:"goal"`
	Dir    string         `json:"dir"`
	Repos  []string       `json:"repos"`
	Counts map[string]int `json:"tasks"`
	Safety *config.Safety `json:"safety,omitempty"`
	// Own are the settings the project sets itself; it follows all
	// projects in the rest (config.Own).
	Own   []string `json:"own_settings,omitempty"`
	Error string   `json:"error,omitempty"`
}

// List returns every project, by slug. A project whose files can't be
// read is listed with its error instead of failing the whole list.
func List() ([]Summary, error) {
	root, err := home.ProjectsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, e := range entries {
		if !e.IsDir() || !ValidSlug(e.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "PROJECT.md")); err != nil {
			continue
		}
		s := Summary{Slug: e.Name(), Dir: filepath.Join(root, e.Name())}
		p, err := OpenWith(e.Name(), cfg)
		if err != nil {
			s.Error = err.Error()
			out = append(out, s)
			continue
		}
		s.Goal, s.Repos = p.Meta.Goal, p.Repos
		if safety, err := cfg.Safety(p.Slug); err == nil {
			s.Safety = &safety
		}
		s.Own = cfg.Own(p.Slug)
		if b, err := p.Tasks().Load(); err != nil {
			s.Error = err.Error()
		} else {
			s.Counts = Counts(b)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// Counts tallies a board by group, keyed "needs_you", "in_motion",
// "on_deck", "backlog", "done" and "dropped".
func Counts(b *tasks.Board) map[string]int {
	c := map[string]int{"needs_you": 0, "in_motion": 0, "on_deck": 0, "backlog": 0, "done": 0, "dropped": 0}
	for _, t := range b.Tasks {
		c[GroupKey(tasks.GroupOf(t.Status))]++
	}
	return c
}

// GroupKey is a group's JSON key.
func GroupKey(g tasks.Group) string {
	return strings.ReplaceAll(strings.ToLower(string(g)), " ", "_")
}

// Resolve picks the project for a command (§6.3): the --project flag,
// then $TERMINATR_PROJECT, then the project whose folder or thread
// worktree contains cwd. It returns "" when none applies.
func Resolve(flag string, getenv func(string) string, cwd string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if s := getenv(caller.EnvProject); s != "" {
		return s, nil
	}
	if cwd == "" {
		return "", nil
	}
	real := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	cwd = real(cwd)
	for _, rootFn := range []func() (string, error){home.ProjectsDir, home.WorktreesDir} {
		root, err := rootFn()
		if err != nil {
			return "", err
		}
		rel, ok := fsx.Within(real(root), cwd)
		if !ok || rel == "." {
			continue
		}
		slug, _, _ := strings.Cut(rel, string(filepath.Separator))
		if ValidSlug(slug) {
			return slug, nil
		}
	}
	return "", nil
}

// SetRepo adds (or removes) a repo in the project's repo list, in
// config.toml (Project.Repos). An added path must be a directory.
// changed is false when it was already so. Adding is the user's: the
// callers refuse an agent.
func (p *Project) SetRepo(path string, add bool) (changed bool, err error) {
	if add {
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			return false, refuse("invalid-repo", "%s is not a directory", path)
		}
	}
	data, err := os.ReadFile(p.Path("PROJECT.md"))
	if err != nil {
		return false, err
	}
	var repos []string
	err = config.UpdateRepos(p.Slug, func(old []string, set bool) ([]string, error) {
		if !set && !reposMoved() {
			old = legacyRepos(data)
		}
		repos, changed = nil, false
		found := false
		for _, r := range old {
			if r == path {
				found = true
				if !add {
					continue
				}
			}
			repos = append(repos, r)
		}
		if add && !found {
			repos = append(repos, path)
		}
		changed = found != add
		return repos, nil
	})
	if err != nil {
		return false, err
	}
	p.Repos = repos
	return changed, nil
}
