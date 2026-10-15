package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/theme"
)

// The live theme (docs/SPEC.md §8.6). Claude watches its config
// folder's themes/ and redraws with a theme there the moment its file
// changes, base included (checked on 2.1.296), while a --settings theme
// and a plugin's themes are read only at start. So tm keeps one theme
// file of its own there, themes/terminatr.json (Claude's
// custom:terminatr), which every Claude it starts selects, and rewrites
// it on every change of the tm theme or the scheme. It is shared: the
// server has one theme and one scheme for all its panes. Nothing else
// in the folder is touched; the user's settings and themes are only
// read.

// ConfigDir is Claude's config folder for home: the status file, task
// snapshots, themes and projects live under it.
func (a *Agent) ConfigDir(home string) string { return configDir(home) }

// configDir is Claude's config folder for home.
func configDir(home string) string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home, ".claude")
}

// builtinThemes are the themes Claude can base one on (2.1.296).
var builtinThemes = []string{"dark", "light", "dark-daltonized", "light-daltonized", "dark-ansi", "light-ansi"}

// themeData is what [theme]'s template sees: the tm theme's colours
// under a pane palette, else the user's own Claude theme as a base and
// overrides.
type themeData struct {
	Theme     *theme.Agent
	Base      string
	Overrides map[string]json.RawMessage
}

// WriteTheme writes Claude's tm theme for t (nil: the user's own theme)
// into its config folder, when its content changes.
func (a *Agent) WriteTheme(home string, t *theme.Agent, scheme string) error {
	if a.m.Theme.Path == "" {
		return errors.New("claude: the manifest has no theme")
	}
	dir := configDir(home)
	data := themeData{Theme: t}
	if t == nil {
		data.Base, data.Overrides = userTheme(dir, strings.TrimSuffix(a.m.Theme.Path, ".json"), scheme)
	}
	s, err := a.m.RenderTheme(data)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, filepath.FromSlash(a.m.Theme.Path))
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, []byte(s)) {
		return nil // a rewrite would only make every Claude redraw
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsx.WriteAtomic(path, []byte(s), 0o644)
}

// userTheme is the user's own Claude theme, as a base and overrides: the
// theme their settings name, a built-in one as it is, one of their own
// themes (custom:<slug>) with its overrides; "auto", none they can see
// (a plugin's) or our own (own, its path without .json) follow the
// scheme. Claude's default, with no theme set, is dark.
func userTheme(dir, own, scheme string) (string, map[string]json.RawMessage) {
	auto := "dark"
	if scheme == "light" {
		auto = "light"
	}
	var settings struct {
		Theme *string `json:"theme"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		_ = json.Unmarshal(b, &settings)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return auto, map[string]json.RawMessage{}
	}
	name := "dark"
	if settings.Theme != nil {
		name = *settings.Theme
	}
	if slices.Contains(builtinThemes, name) {
		return name, map[string]json.RawMessage{}
	}
	slug, ok := strings.CutPrefix(name, "custom:")
	if !ok || slug == "" || strings.ContainsAny(slug, `:/\`) || "themes/"+slug == own {
		return auto, map[string]json.RawMessage{}
	}
	var t struct {
		Base      string                     `json:"base"`
		Overrides map[string]json.RawMessage `json:"overrides"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "themes", slug+".json"))
	if err != nil || json.Unmarshal(b, &t) != nil {
		return auto, map[string]json.RawMessage{}
	}
	base := "dark" // Claude's, for a base it doesn't know
	if slices.Contains(builtinThemes, t.Base) {
		base = t.Base
	}
	if t.Overrides == nil {
		t.Overrides = map[string]json.RawMessage{}
	}
	return base, t.Overrides
}
