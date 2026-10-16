package models

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

// Windows names the variable "Path", and os.Environ returns it so.
func TestLookPathEnvNameCase(t *testing.T) {
	if !caps.Windows {
		t.Skip("env names are case-sensitive here")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gem.cmd"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := &agent.Manifest{}
	m.Launch.Command = "gem"
	got, err := LookPath(m, []string{"Path=" + dir})
	if err != nil || filepath.Dir(got) != dir {
		t.Errorf("Path=: got %q, %v", got, err)
	}
}
