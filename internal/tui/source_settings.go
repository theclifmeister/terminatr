package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// ServerSource's SettingsSource: settings, repositories and models.

func (s *ServerSource) SetSetting(table, key string, value any) error {
	defer s.touch()
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	if table == config.DefaultsTable {
		var err error
		if value == nil {
			err = config.UnsetDefaults(key)
		} else {
			err = config.SetDefaults(key, value)
		}
		if err != nil {
			return err
		}
		journalAll(s.Caller, key, value, "all projects")
		return nil
	}
	slug, isProject := strings.CutPrefix(table, "projects.")
	if !isProject {
		if value == nil {
			return config.Unset(table, key)
		}
		return config.Set(table, key, value)
	}
	detail := fmt.Sprint(value)
	if value == nil {
		if err := config.UnsetProject(slug, key); err != nil {
			return err
		}
		detail = "(follows all projects)"
	} else if err := config.SetProject(slug, key, value); err != nil {
		return err
	}
	// The project's journal records the change, as every tm action.
	if p, err := project.Open(slug); err == nil {
		p.Journal(s.Caller, "settings."+key, slug, detail)
	}
	return nil
}

// ApplyModDefaults applies the defaults enabled mod id suggests for all
// projects (mods design §7, Tighten only), on the human's word: each in
// [defaults], journaled as the human's change, except a key config.toml
// already sets there (the human's stays) or one already at the value.
// It returns the suggestions applied.
func ApplyModDefaults(c caller.Caller, id string) ([]mod.Suggestion, error) {
	if c.IsAgent() {
		return nil, errHumanOnly
	}
	cfg, err := config.Load()
	if err != nil && cfg == nil {
		return nil, err
	}
	r, err := mod.Load(cfg)
	if err != nil {
		return nil, err
	}
	md, ok := r.Get(id)
	switch {
	case !ok:
		return nil, fmt.Errorf("no mod %q is installed", id)
	case md.State != mod.Enabled || md.Manifest == nil:
		return nil, fmt.Errorf("%s is off: its defaults apply once it is enabled", id)
	}
	var done []mod.Suggestion
	for _, sg := range md.Manifest.Suggestions(cfg) {
		if !sg.Applies() {
			continue
		}
		if err := config.SetDefaults(sg.Key, sg.Value); err != nil {
			return done, fmt.Errorf("%s: %w", sg.Key, err)
		}
		journalAll(c, sg.Key, sg.Value, "all projects, suggested by mod:"+id)
		done = append(done, sg)
	}
	return done, nil
}

func (s *ServerSource) ApplyModDefaults(id string) ([]mod.Suggestion, error) {
	defer s.touch()
	return ApplyModDefaults(s.Caller, id)
}

// journalAll records a change of an all-projects setting in the journal
// of each project that follows it, i.e. doesn't set key itself; why
// follows the value in brackets.
func journalAll(c caller.Caller, key string, value any, why string) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if key == "auto_resolve" {
		key = "auto_close"
	}
	list, _ := project.List()
	for _, sum := range list {
		if slices.Contains(cfg.Own(sum.Slug), key) {
			continue
		}
		if p, err := project.Open(sum.Slug); err == nil {
			p.Journal(c, "settings."+key, sum.Slug, fmt.Sprint(value)+" ("+why+")")
		}
	}
}

func (s *ServerSource) SetRepo(slug, path string, add bool) error {
	defer s.touch()
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return err
	}
	changed, err := p.SetRepo(path, add)
	if err != nil || !changed {
		return err
	}
	verb := map[bool]string{true: "add", false: "remove"}[add]
	return p.Journal(s.Caller, "project.repo."+verb, slug, path)
}

// Catalog is one installed agent's models (models.Catalog).
type Catalog = models.Catalog

// catalogNames lists the models the catalogs offer, each name once: the
// choices of the Thread models setting.
func catalogNames(cats []Catalog) []string {
	var out []string
	for _, c := range cats {
		for _, n := range c.Names() {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

func (s *ServerSource) Catalogs() []Catalog {
	reg, _ := agent.Load(s.Paths.AgentsDir())
	if reg == nil {
		return nil
	}
	cfg, _ := config.Load()
	return models.Catalogs(reg, cfg, models.Installed(reg, nil))
}

func (s *ServerSource) SetModels(agentName string, set config.AgentSettings) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	return config.SetAgentModels(agentName, set)
}

func (s *ServerSource) SyncModel(session string) error {
	var res proto.SessionModelResult
	return s.call(proto.MethodSessionModel, proto.SessionModelParams{ID: session}, &res)
}

func (s *ServerSource) RefreshModels(agentName string) error {
	reg, _ := agent.Load(s.Paths.AgentsDir())
	if reg == nil {
		return errors.New("the agent manifests don't load")
	}
	a, ok := reg.Get(agentName)
	m := agent.ManifestOf(a)
	if !ok || m == nil {
		return fmt.Errorf("no agent %s", agentName)
	}
	_, err := models.Refresh(context.Background(), m, nil)
	return err
}

func (s *ServerSource) Unrefuse(agentName, model string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	return models.Unrefuse(agentName, model)
}
