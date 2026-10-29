package cli

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/project"
)

// modSettingRow is one of a mod's settings in tm mod settings --json.
type modSettingRow struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Value   any      `json:"value"`
	From    string   `json:"from"` // default, all projects, project
	Default any      `json:"default"`
	Options []string `json:"options,omitempty"`
	Min     *int64   `json:"min,omitempty"`
	Max     *int64   `json:"max,omitempty"`
	Label   string   `json:"label,omitempty"`
}

// modSettingsOut is tm mod settings --json.
type modSettingsOut struct {
	Mod      string          `json:"mod"`
	Project  string          `json:"project,omitempty"`
	Settings []modSettingRow `json:"settings"`
	Bad      []string        `json:"bad,omitempty"`
}

// modSettings is tm mod settings (mod API 1.11): mod id's settings, for
// all projects or for project slug, each with where its value comes
// from; with sets (key=value) or reset (a key) it changes them first,
// the human's call, every value checked before any is written. An
// agent's own project is the one shown when it names none.
func (e *Env) modSettings(id, slug, reset string, sets []string, asJSON bool) error {
	change := len(sets) > 0 || reset != ""
	if change && e.Caller.IsAgent() {
		return humanOnly()
	}
	if slug == "" && !change && e.Caller.IsAgent() {
		slug = e.Caller.Project
	}
	if err := optProject(slug); err != nil {
		return err
	}
	r, err := registry()
	if err != nil {
		return err
	}
	m, ok := r.Get(id)
	if !ok {
		return &project.Error{Code: "no-mod", Msg: "no mod " + id + ": tm mod list shows the installed ones"}
	}
	if m.Manifest == nil {
		return modErr(fmt.Errorf("%s has no settings: %w", id, m.Err))
	}
	if change {
		type kv struct {
			key string
			v   any
		}
		var vals []kv
		for _, a := range sets {
			key, raw, ok := strings.Cut(a, "=")
			if !ok || key == "" {
				return usagef("%q isn't <key>=<value>\n%s", a, modUsage)
			}
			s, ok := m.Manifest.Settings[key]
			if !ok {
				return modErr(fmt.Errorf("%s has no setting %q", id, key))
			}
			v, err := parseSetting(s, raw)
			if err == nil {
				v, err = mod.CheckSetting(m.Manifest, key, v)
			}
			if err != nil {
				return modErr(err)
			}
			vals = append(vals, kv{key, v})
		}
		if reset != "" {
			if _, ok := m.Manifest.Settings[reset]; !ok {
				return modErr(fmt.Errorf("%s has no setting %q", id, reset))
			}
			vals = append(vals, kv{reset, nil})
		}
		for _, x := range vals {
			if err := mod.SetSettingFor(e.Caller, id, slug, x.key, x.v); err != nil {
				return modErr(err)
			}
		}
		if r, err = registry(); err != nil {
			return err
		}
		m, _ = r.Get(id)
	}
	out := modSettingsOut{Mod: id, Project: slug, Settings: []modSettingRow{}, Bad: m.BadSettings}
	for _, key := range slices.Sorted(maps.Keys(m.Manifest.Settings)) {
		s := m.Manifest.Settings[key]
		def, _ := s.Check(s.Default)
		row := modSettingRow{Key: key, Type: s.Type, Value: m.SettingsFor(slug)[key], From: "default", Default: def,
			Options: s.Options, Min: s.Min, Max: s.Max, Label: mod.Clean(s.Label, false)}
		if _, ok := m.ProjectSettings[slug][key]; ok && slug != "" {
			row.From = "project"
		} else if row.Value != def {
			row.From = "all projects"
		}
		out.Settings = append(out.Settings, row)
	}
	if asJSON {
		return e.printJSON(out)
	}
	if len(out.Settings) == 0 {
		fmt.Fprintf(e.Stdout, "%s has no settings\n", id)
		return nil
	}
	fmt.Fprintf(e.Stdout, "%s settings for %s\n", id, cmp.Or(slug, "all projects"))
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	for _, row := range out.Settings {
		what := row.Type
		if row.Type == mod.SettingChoice {
			opts := make([]string, len(row.Options))
			for i, o := range row.Options {
				opts[i] = mod.Clean(o, false)
			}
			what = strings.Join(opts, "|")
		}
		label := ""
		if row.Label != "" {
			label = " · " + row.Label
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t(%s)%s\n", row.Key, mod.SettingWords(row.Value), row.From, what, label)
	}
	tw.Flush()
	for _, b := range out.Bad {
		fmt.Fprintf(e.Stdout, "ignored: %s\n", mod.Clean(b, false))
	}
	return nil
}

// parseSetting reads raw as setting s's type: a whole number, true or
// false, or the text as it is.
func parseSetting(s mod.Setting, raw string) (any, error) {
	switch s.Type {
	case mod.SettingInt:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q isn't a whole number", raw)
		}
		return n, nil
	case mod.SettingBool:
		b, err := strconv.ParseBool(raw)
		if err != nil || raw != "true" && raw != "false" {
			return nil, fmt.Errorf("%q isn't true or false", raw)
		}
		return b, nil
	}
	return raw, nil
}
