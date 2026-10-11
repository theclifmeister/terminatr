package tui

import (
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/moddev"
)

// TestModScaffoldsLoad: tm mod new's theme, icons and keymap load as the
// dashboard reads them.
func TestModScaffoldsLoad(t *testing.T) {
	scaffold := func(kind string) map[string]string {
		t.Helper()
		f, err := moddev.Scaffold("x", kind)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	if _, err := parseTheme([]byte(scaffold("theme")["theme.toml"])); err != nil {
		t.Errorf("theme: %v", err)
	}
	if _, err := parseIcons([]byte(scaffold("icons")["icons.toml"])); err != nil {
		t.Errorf("icons: %v", err)
	}
	var km map[string][]string
	if _, err := toml.Decode(scaffold("keymap")["keymap.toml"], &km); err != nil {
		t.Fatalf("keymap: %v", err)
	}
	defer func() {
		if err := ApplyKeymap(nil, ""); err != nil {
			t.Fatal(err)
		}
	}()
	if err := ApplyKeymap(km, "ctrl+b"); err != nil {
		t.Errorf("keymap: %v", err)
	}
}
