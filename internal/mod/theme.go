package mod

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Themes (theme.toml; docs/SPEC.md §5.3). The console (tui) reads the
// roles; this package checks only what the mod API gates.

// PaneAPIMinor is the mod API minor a theme's pane palette ([light.pane],
// [dark.pane]) needs.
const PaneAPIMinor = 6

// ThemeCheckAPI says why a mod written for mod API api can't carry the
// theme: a pane palette needs 1.6, so that an older tm refuses the mod by
// its api (CheckAPI) rather than by an unknown key. A theme that doesn't
// parse passes here (CheckData says why).
func ThemeCheckAPI(data []byte, api string) error {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil
	}
	for _, scheme := range []string{"light", "dark"} {
		t, _ := raw[scheme].(map[string]any)
		if _, ok := t["pane"]; !ok {
			continue
		}
		if maj, min, ok := splitAPI(api); ok && maj == 1 && min < PaneAPIMinor {
			return fmt.Errorf(`[%s.pane] needs api = "1.%d" in mod.toml`, scheme, PaneAPIMinor)
		}
	}
	return nil
}

// checkThemeAPI is ThemeCheckAPI for the mod's theme, which tm mod
// install asks with the manifest's: nil when it has none, or one that
// can't be read (CheckData says why).
func (m Mod) checkThemeAPI() error {
	if !m.Has(KindTheme) {
		return nil
	}
	data, err := m.Data(KindTheme)
	if err != nil {
		return nil
	}
	if err := ThemeCheckAPI(data, m.Manifest.API); err != nil {
		return fmt.Errorf("%s: %w", KindTheme, err)
	}
	return nil
}
