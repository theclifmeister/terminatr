package shell

import (
	"slices"
	"testing"
)

func TestEnvValue(t *testing.T) {
	env := []string{"HOME=/h", "Path=C:\\a", "PATH=/b"}
	for _, tc := range []struct {
		win  bool
		key  string
		want string
		ok   bool
	}{
		{true, "PATH", "/b", true},
		{true, "path", "/b", true},
		{true, "NOPE", "", false},
		{false, "PATH", "/b", true},
		{false, "Path", "C:\\a", true},
	} {
		got, ok := envValue(tc.win, env, tc.key)
		if got != tc.want || ok != tc.ok {
			t.Errorf("win=%v %q: got %q, %v", tc.win, tc.key, got, ok)
		}
	}
	if got, _ := envValue(true, []string{"Path=C:\\a"}, "PATH"); got != "C:\\a" {
		t.Errorf("windows Path=: got %q", got)
	}
	if got, ok := envValue(false, []string{"Path=C:\\a"}, "PATH"); ok || got != "" {
		t.Errorf("elsewhere Path= is another variable: got %q, %v", got, ok)
	}
}

func TestSetEnvPath(t *testing.T) {
	add := func(old string) string { return "/bin:" + old }
	tests := []struct {
		win  bool
		env  []string
		want []string
	}{
		{true, []string{"HOME=/h", "Path=/x"}, []string{"HOME=/h", "Path=/bin:/x"}},
		{true, []string{"Path=/x", "PATH=/y"}, []string{"Path=/bin:/y"}},
		{true, []string{"HOME=/h"}, []string{"HOME=/h", "PATH=/bin:"}},
		{false, []string{"Path=/x"}, []string{"Path=/x", "PATH=/bin:"}},
		{false, []string{"PATH=/x"}, []string{"PATH=/bin:/x"}},
	}
	for _, tc := range tests {
		if got := setEnvPath(tc.win, tc.env, add); !slices.Equal(got, tc.want) {
			t.Errorf("win=%v %v: got %v, want %v", tc.win, tc.env, got, tc.want)
		}
	}
}
