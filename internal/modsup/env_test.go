package modsup

import (
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// TestHostEnv checks a host gets the bare environment and the malloc
// settings (T360), and none of the server's other variables.
func TestHostEnv(t *testing.T) {
	kv := map[string]string{"HOME": "/home/u", "GH_TOKEN": "x", "MALLOC_ARENA_MAX": "4"}
	getenv := func(k string) string { return kv[k] }
	want := append(proc.BareEnv(), proc.MallocEnv(getenv)...)
	if got := hostEnv(getenv); !slices.Equal(got, want) {
		t.Errorf("hostEnv = %q, want %q", got, want)
	}
	for _, e := range hostEnv(getenv) {
		if e == "HOME=/home/u" || e == "GH_TOKEN=x" {
			t.Errorf("hostEnv passes on %s", e)
		}
	}
}
