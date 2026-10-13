package agent

import "testing"

func TestThemeFuncs(t *testing.T) {
	for _, c := range []struct {
		a, b string
		pct  int
		want string
	}{
		{"#000000", "#ffffff", 50, "#808080"},
		{"#2e3440", "#d8dee9", 0, "#2e3440"},
		{"#2e3440", "#d8dee9", 100, "#d8dee9"},
		{"", "#ffffff", 50, ""},
		{"#000000", "red", 50, ""},
	} {
		if got := blend(c.a, c.b, c.pct); got != c.want {
			t.Errorf("blend %s %s %d = %q, want %q", c.a, c.b, c.pct, got, c.want)
		}
	}
	if got, err := colors("error", "#bf616a", "success", "", "x\"y", "#000000"); err != nil || got != `{"error":"#bf616a","x\"y":"#000000"}` {
		t.Errorf("colors = %s, %v", got, err)
	}
	if got, _ := colors(); got != "{}" {
		t.Errorf("colors() = %s", got)
	}
	if _, err := colors("odd"); err == nil {
		t.Error("an odd number of values")
	}
}
