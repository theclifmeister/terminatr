package mod

import (
	"strings"
	"testing"
)

// TestThemeAPI: a theme's pane palette needs mod API 1.6, naming its
// table; roles alone go with any 1.x.
func TestThemeAPI(t *testing.T) {
	for _, tc := range []struct{ toml, api, want string }{
		{"[dark]\naccent = { fg = [\"cyan\"] }", "1.0", ""},
		{"[dark.pane]\nbg = \"#000000\"", "1.5", `[dark.pane] needs api = "1.6" in mod.toml`},
		{"[light]\npane = { fg = \"#000000\" }", "1.0", `[light.pane] needs api = "1.6"`},
		{"[dark.pane]\nbg = \"#000000\"", "1.6", ""},
		{"not toml [", "1.0", ""}, // CheckData says why it doesn't parse
	} {
		err := ThemeCheckAPI([]byte(tc.toml), tc.api)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s under %s: %v, want %q", tc.toml, tc.api, err, tc.want)
		}
	}
}

// TestInstallThemeAPI: tm mod install refuses a pane palette in a mod
// that says api 1.5, and CheckData says so; at 1.6 both take it.
func TestInstallThemeAPI(t *testing.T) {
	withHome(t)
	files := func(api string) map[string]string {
		return map[string]string{
			"mod.toml":   strings.Replace(dataMod("dusk", `theme = "theme.toml"`), `api = "1.0"`, `api = "`+api+`"`, 1),
			"theme.toml": "[dark.pane]\nbg = \"#101010\"\n",
		}
	}
	src := install(t, t.TempDir(), "dusk", files("1.5"))
	st, err := Fetch(Source{Path: src})
	if st != nil {
		st.Close()
	}
	if err == nil || !strings.Contains(err.Error(), `theme: [dark.pane] needs api = "1.6" in mod.toml`) {
		t.Fatalf("api 1.5: %v", err)
	}
	m, err := ReadManifest(src)
	if err != nil {
		t.Fatal(err)
	}
	if errs := (Mod{ID: "dusk", Dir: src, Manifest: m}).CheckData(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "needs api") {
		t.Fatalf("CheckData at 1.5: %v", errs)
	}
	src = install(t, t.TempDir(), "dusk", files("1.6"))
	if _, err := Validate(src); err != nil {
		t.Fatalf("api 1.6: %v", err)
	}
}
