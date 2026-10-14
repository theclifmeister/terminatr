package mod

import (
	"strings"
	"testing"
)

// TestCheckText: a text setting is one line of text, nothing that
// textsafe would take out of it.
func TestCheckText(t *testing.T) {
	for _, bad := range []string{"a\nb", "a\tb", "a\x1b[31mb", "a\u202eb", "a\u200bb", "a\u2028b", "a\ufffdb", strings.Repeat("x", MaxTextSetting+1)} {
		if checkText(bad) == nil {
			t.Errorf("checkText(%q) passed", bad)
		}
	}
	for _, ok := range []any{"plain text", "caf\u00e9 \u65e5\u672c", strings.Repeat("x", MaxTextSetting), 3, true} {
		if err := checkText(ok); err != nil {
			t.Errorf("checkText(%v): %v", ok, err)
		}
	}
}
