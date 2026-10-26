package agent

import (
	"slices"
	"testing"
)

const permScreen = `  Bash command

    git push

  Do you want to proceed?
  ❯ 1. Yes
    2. Yes, and don't ask again for git push commands in /src
    3. No, and tell Claude what to do differently (esc)
`

func TestDialog(t *testing.T) {
	opt, q := DialogOption(permScreen, 1)
	if opt != "Yes" || q != "Do you want to proceed?" {
		t.Errorf("option 1: %q, %q", opt, q)
	}
	if opt, _ := DialogOption(permScreen, 4); opt != "" {
		t.Errorf("option 4: %q", opt)
	}
	want := []string{"Yes", "Yes, and don't ask again for git push commands in /src", "No, and tell Claude what to do differently (esc)"}
	if got := DialogOptions(permScreen); !slices.Equal(got, want) {
		t.Errorf("options %q", got)
	}
	if DialogOptions("no dialog here") != nil {
		t.Error("options without a dialog")
	}
	for opt, want := range map[string]bool{"Yes": false, want[1]: true, "Yes, always allow": true, "Yes, and don’t ask again": true, want[2]: false} {
		if AllowsAlways(opt) != want {
			t.Errorf("AllowsAlways(%q) = %v", opt, !want)
		}
	}
}
