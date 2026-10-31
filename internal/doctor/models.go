package doctor

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/models"
)

// Models checks which agents are installed and the models each one
// offers (docs/SPEC.md §8.2, §11.2): it asks each installed agent again
// (Deps.ProbeModels), then reports per agent what it offers, or why
// its models are unknown. It fails when no agent is installed or a
// setting names an agent that isn't, and warns about settings the
// agents' answers no longer back: models and coordinator_model names
// no agent offers, a default_model, the older models list.
func Models(d Deps) []Check {
	cfg, err := config.Load()
	if err != nil {
		return nil // Settings reports it
	}
	reg, _ := agent.Load(d.Paths.AgentsDir())
	if reg == nil {
		return nil // Agents reports it
	}
	out, cats, installed := catalogChecks(d, reg, cfg)
	if len(installed) == 0 {
		return out
	}
	out = append(out, agentSettingsChecks(cfg, reg, cats)...)
	scoped, stale := scopeChecks(cfg, reg, installed, cats)
	out = append(out, scoped...)
	out = append(out, severalAgentsChecks(cfg, installed)...)
	return append(out, staleModelChecks(stale)...)
}

// modelsWarn is a Warn check in the models row.
func modelsWarn(format string, args ...any) Check {
	return Check{Group: "agents", Name: "models", Status: Warn, Detail: fmt.Sprintf(format, args...)}
}

// agentFail is a Fail check in the agent row.
func agentFail(format string, args ...any) Check {
	return Check{Group: "agents", Name: "agent", Status: Fail, Detail: fmt.Sprintf(format, args...)}
}

// catalogChecks finds the installed agents, asks each for its models again
// and reports one line per agent: what it offers, or why that is unknown.
// With no agent installed it returns the one failure.
func catalogChecks(d Deps, reg *agent.Registry, cfg *config.Config) (out []Check, cats []models.Catalog, installed []string) {
	const g = "agents"
	for _, name := range reg.Names() {
		a, _ := reg.Get(name)
		if m := agent.ManifestOf(a); m != nil && d.LookPath != nil {
			if _, err := d.LookPath(m.Launch.Command); err == nil {
				installed = append(installed, name)
			}
		}
	}
	if len(installed) == 0 {
		return append(out, agentFail("%s", errMsg(models.NoAgent(reg)))), nil, nil
	}
	now := time.Now()
	for _, name := range installed {
		a, _ := reg.Get(name)
		m := agent.ManifestOf(a)
		if d.ProbeModels != nil && m.ListModels != nil {
			_, _ = d.ProbeModels(m)
		}
		c := models.Get(m, cfg, true)
		cats = append(cats, c)
		ck := Check{Group: g, Name: name + " models"}
		if !c.Known {
			ck.Status = Warn
			ck.Detail = fmt.Sprintf("unknown: %s. Threads run %s's own default and --model is refused", c.Reason, name)
			if c.HasCache && c.Cache.Status == models.StatusLoggedOut {
				ck.Detail += fmt.Sprintf("; log in to %s to see your account's models", m.Launch.Command)
			}
			out = append(out, ck)
			continue
		}
		ck.Status = OK
		ck.Detail = fmt.Sprintf("%d models, %s", len(c.Models), c.Source(now))
		var extra []string
		if n := len(c.Hidden); n > 0 {
			extra = append(extra, fmt.Sprintf("hidden by you: %d", n))
		}
		var yours []string
		for _, x := range c.Models {
			if x.Yours {
				yours = append(yours, x.Name)
			}
		}
		if len(yours) > 0 {
			extra = append(extra, "yours: "+strings.Join(yours, ", "))
		}
		for _, x := range c.Refused {
			extra = append(extra, fmt.Sprintf("refused for your account: %s (%s)", x.Name, x.Refusal.Reason))
		}
		if len(extra) > 0 {
			ck.Detail += "; " + strings.Join(extra, "; ")
		}
		out = append(out, ck)
	}
	return out, cats, installed
}

// catalogOf is the catalog of the named agent.
func catalogOf(cats []models.Catalog, name string) (models.Catalog, bool) {
	for _, c := range cats {
		if c.Agent == name {
			return c, true
		}
	}
	return models.Catalog{}, false
}

// agentSettingsChecks warns about [agents.<name>] settings that name an
// unknown agent, use the older models list or a default model no longer
// offered, and about manifests that still carry [[models]].
func agentSettingsChecks(cfg *config.Config, reg *agent.Registry, cats []models.Catalog) []Check {
	var out []Check
	for _, name := range cfg.AgentNames() {
		if _, ok := reg.Get(name); !ok {
			out = append(out, modelsWarn("config.toml has models settings for agent %s, which tm doesn't know (tm agent list)", name))
			continue
		}
		s := cfg.Agent(name)
		if s.HasLegacy {
			out = append(out, modelsWarn("config.toml [agents.%s] models is the older full list: tm now asks %s for its models; the names it doesn't list count as yours, and Settings > Models saves the new form", name, name))
		}
		c, ok := catalogOf(cats, name)
		if ok && c.Known && s.DefaultModel != "" && !c.Has(s.DefaultModel) {
			out = append(out, modelsWarn("%s's default model %s isn't offered now, so threads run %s's own default; pick another in Settings > Models", name, s.DefaultModel, name))
		}
	}
	for _, name := range reg.Names() {
		a, _ := reg.Get(name)
		if m := agent.ManifestOf(a); m != nil && len(m.LegacyModels) > 0 {
			out = append(out, modelsWarn("%s's manifest (%s) has [[models]], which tm no longer reads: tm asks the agent ([list_models], docs/SPEC.md §8.2)", name, reg.Source[name]))
		}
	}
	return out
}

// scopeChecks checks the settings that name agents and models, for all
// projects and each project with its own. stale maps each model name no
// installed agent offers to the scopes that list it.
func scopeChecks(cfg *config.Config, reg *agent.Registry, installed []string, cats []models.Catalog) (out []Check, stale map[string][]string) {
	type scope struct {
		who    string
		safety config.Safety
		own    []string
	}
	var scopes []scope
	if all, err := cfg.AllProjects(); err == nil {
		scopes = append(scopes, scope{config.AllProjectsName, all, []string{"thread_agent", "thread_agents", "coordinator_agent", "models", "coordinator_model"}})
	}
	for _, slug := range cfg.ProjectSlugs() {
		if s, err := cfg.Safety(slug); err == nil {
			scopes = append(scopes, scope{slug, s, cfg.Own(slug)})
		}
	}
	stale = map[string][]string{}
	for _, sc := range scopes {
		for _, k := range []struct{ key, value, what string }{
			{"thread_agent", sc.safety.ThreadAgent, "thread agent"},
			{"coordinator_agent", sc.safety.CoordinatorAgent, "coordinator agent"},
		} {
			if !slices.Contains(sc.own, k.key) {
				continue
			}
			if _, _, err := models.Resolve(reg, installed, k.value, k.what); err != nil && k.value != "" {
				out = append(out, agentFail("%s: %s %s", sc.who, k.key, errMsg(err)))
			}
		}
		if slices.Contains(sc.own, "thread_agents") {
			for _, n := range sc.safety.ThreadAgents {
				if _, _, err := models.Resolve(reg, installed, n, "thread agent"); err != nil {
					out = append(out, modelsWarn("%s: thread_agents %s: %s", sc.who, n, errMsg(err)))
				}
			}
		}
		if slices.Contains(sc.own, "models") {
			for _, n := range sc.safety.Models {
				offered := false
				for _, c := range cats {
					for _, m := range c.Names() {
						offered = offered || config.ScopeTakes([]string{n}, c.Agent, m)
					}
				}
				if !offered {
					stale[n] = append(stale[n], sc.who)
				}
			}
		}
		if slices.Contains(sc.own, "coordinator_model") && sc.safety.CoordinatorModel != "" {
			name, _, err := models.Resolve(reg, installed, sc.safety.CoordinatorAgent, "coordinator agent")
			if c, ok := catalogOf(cats, name); err == nil && ok && c.Known {
				if err := c.Check(sc.safety.CoordinatorModel, sc.safety.Models); err != nil {
					out = append(out, modelsWarn("%s: coordinator_model %s: %s; the coordinator runs %s's default", sc.who, sc.safety.CoordinatorModel, errMsg(err), name))
				}
			}
		}
	}
	return out, stale
}

// severalAgentsChecks warns when several agents are installed and the
// settings for all projects name no thread or coordinator agent.
func severalAgentsChecks(cfg *config.Config, installed []string) []Check {
	if len(installed) < 2 {
		return nil
	}
	all, _ := cfg.AllProjects()
	if all.DefaultThreadAgent() != "" && all.CoordinatorAgent != "" {
		return nil
	}
	return []Check{modelsWarn("several agents are installed (%s) and the settings name none for all projects: the dashboard asks once when a project opens; until then tm thread start needs --agent", strings.Join(installed, ", "))}
}

// staleModelChecks warns, by model name, about models no installed agent
// offers now.
func staleModelChecks(stale map[string][]string) []Check {
	var names []string
	for n := range stale {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Check
	for _, n := range names {
		out = append(out, modelsWarn("model %s is in the models of %s, but no installed agent offers it now; --model %s is refused", n, strings.Join(stale[n], ", "), n))
	}
	return out
}

// errMsg is a models refusal's message without its code.
func errMsg(err error) string {
	if me, ok := err.(*models.Error); ok {
		return me.Msg
	}
	return err.Error()
}
