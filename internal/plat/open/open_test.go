package open

import (
	"reflect"
	"testing"
)

func TestCommand(t *testing.T) {
	const u = "https://x.test/a?b=1&c=2"
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	for _, c := range []struct {
		goos string
		wsl  bool
		have func(string) bool
		name string
		args []string
	}{
		{"darwin", false, no, "open", []string{u}},
		{"linux", false, yes, "xdg-open", []string{u}},
		{"linux", true, yes, "wslview", []string{u}},
		{"linux", true, no, "explorer.exe", []string{u}},
		{"windows", false, no, "rundll32", []string{"url.dll,FileProtocolHandler", u}},
	} {
		name, args := command(c.goos, u, c.wsl, c.have)
		if name != c.name || !reflect.DeepEqual(args, c.args) {
			t.Errorf("%s wsl=%v: %q %q, want %q %q", c.goos, c.wsl, name, args, c.name, c.args)
		}
	}
}

func TestWeb(t *testing.T) {
	for s, want := range map[string]bool{
		"https://github.com/o/r/pull/1": true,
		"http://x.test/a":               true,
		"":                              false,
		"file:///tmp/x.command":         false,
		`C:\Users\x\evil.exe`:           false,
		"smb://host/share/x.exe":        false,
		"-a Calculator":                 false,
		"-https://x.test":               false,
		"https:///nohost":               false,
		"https://":                      false,
		"/usr/bin/true":                 false,
		" https://x.test":               false,
		"javascript:alert(1)":           false,
	} {
		if got := Web(s); got != want {
			t.Errorf("Web(%q) = %v, want %v", s, got, want)
		}
	}
	if err := URL("file:///tmp/x.command"); err == nil {
		t.Error("URL opened a non-web address")
	}
}
