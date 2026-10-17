package open

import (
	"runtime"
	"strings"
)

// Pager is the pager's command line for a file, at its end: $PAGER
// (split on white space, as "less -R"), else less +G (more on Windows).
func Pager(getenv func(string) string, path string) []string {
	if f := strings.Fields(getenv("PAGER")); len(f) > 0 {
		return append(f, path)
	}
	if runtime.GOOS == "windows" {
		return []string{"more", path}
	}
	return []string{"less", "+G", path}
}
