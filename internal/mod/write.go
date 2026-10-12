package mod

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
)

// Writing [mods.<id>] (mods design §7, Where grants live). Enabling a
// mod, granting it permissions, limiting it to projects and changing its
// settings are human calls, as project.active is: an agent caller is
// refused. The coordinator may suggest a mod in chat; it can't turn one
// on. Each write edits config.toml line by line (config.Set).

// ErrHumanOnly refuses a mod change from inside an agent.
var ErrHumanOnly = errors.New("human-only: mods are enabled, granted and set by the human")

// installed returns the mod id from the registry in ~/.terminatr/mods,
// with the config.toml it was joined with.
func installed(id string) (Mod, error) {
	c, err := config.Load()
	if err != nil && c == nil {
		return Mod{}, err
	}
	r, err := Load(c)
	if err != nil {
		return Mod{}, err
	}
	m, ok := r.Get(id)
	if !ok {
		return Mod{}, fmt.Errorf("no mod %q is installed", id)
	}
	return m, nil
}

// SetEnabled enables or disables mod id. Enabling refuses a mod that is
// invalid or needs another mod API, with the reason.
func SetEnabled(c caller.Caller, id string, on bool) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	m, err := installed(id)
	if err != nil {
		return err
	}
	if on && (m.State == Invalid || m.State == Incompatible) {
		return fmt.Errorf("%s can't be enabled: %w", id, m.Err)
	}
	if !on && m.Required {
		return ErrRequired{id}
	}
	return config.Set(config.ModsTable(id), "enabled", on)
}

// SetGrants sets the permissions the human grants mod id: names from
// Perms that its manifest asks for, in Perms order. None writes an empty
// list.
func SetGrants(c caller.Caller, id string, grants []string) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	m, err := installed(id)
	if err != nil {
		return err
	}
	if m.Manifest == nil {
		return fmt.Errorf("%s can't be granted anything: %w", id, m.Err)
	}
	for _, g := range grants {
		if !slices.Contains(Perms, g) {
			return fmt.Errorf("no permission is called %q", g)
		}
	}
	asked := m.Manifest.Permissions.Requested()
	out := []string{}
	for _, p := range Perms {
		if !slices.Contains(grants, p) {
			continue
		}
		if !slices.Contains(asked, p) {
			return fmt.Errorf("%s doesn't ask for %s", id, p)
		}
		out = append(out, p)
	}
	return config.Set(config.ModsTable(id), "grants", out)
}

// SetProjects limits mod id to projects; none removes the limit, so it
// runs for every project.
func SetProjects(c caller.Caller, id string, projects []string) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	if _, err := installed(id); err != nil {
		return err
	}
	if len(projects) == 0 {
		return config.Unset(config.ModsTable(id), "projects")
	}
	seen := map[string]bool{}
	for _, p := range projects {
		if p == "" || seen[p] {
			return fmt.Errorf("projects: %q is empty or listed twice", p)
		}
		seen[p] = true
	}
	return config.Set(config.ModsTable(id), "projects", projects)
}

// MaxTextSetting caps a text setting the human sets, in characters.
const MaxTextSetting = 500

// checkText refuses a text value that is too long or has a control
// character (a newline among them): a setting is one line.
func checkText(v any) error {
	str, ok := v.(string)
	if !ok {
		return nil
	}
	if utf8.RuneCountInString(str) > MaxTextSetting {
		return fmt.Errorf("must be at most %d characters", MaxTextSetting)
	}
	if strings.ContainsFunc(str, unicode.IsControl) {
		return fmt.Errorf("must be one line, without control characters")
	}
	return nil
}

// SetSetting sets one of mod id's own options, checked against its
// manifest; a nil value removes it, back to the default.
func SetSetting(c caller.Caller, id, key string, value any) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	m, err := installed(id)
	if err != nil {
		return err
	}
	if m.Manifest == nil {
		return fmt.Errorf("%s has no settings: %w", id, m.Err)
	}
	s, ok := m.Manifest.Settings[key]
	if !ok {
		return fmt.Errorf("%s has no setting %q", id, key)
	}
	table := config.ModsTable(id) + ".settings"
	if value == nil {
		return config.Unset(table, key)
	}
	v, err := s.Check(value)
	if err == nil {
		err = checkText(v)
	}
	if err != nil {
		return fmt.Errorf("%s %v", key, err)
	}
	if n, ok := v.(int64); ok {
		v = int(n)
	}
	return config.Set(table, key, v)
}
