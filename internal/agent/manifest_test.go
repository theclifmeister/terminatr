package agent

import "testing"

func TestHookexecFunc(t *testing.T) {
	f := funcs["hookexec"].(func(string, ...string) (string, error))
	got, err := f(`C:\Program Files\tm.exe`, "hook", "--agent", "claude")
	want := `"command":"C:\\Program Files\\tm.exe","args":["hook","--agent","claude"]`
	if err != nil || got != want {
		t.Fatalf("hookexec = %s, %v; want %s", got, err, want)
	}
	if got, _ := f("/bin/tm"); got != `"command":"/bin/tm","args":[]` {
		t.Fatalf("hookexec without args = %s", got)
	}
}

func TestPosixPath(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\a\p`: "/c/Users/a/p",
		`d:/x`:         "/d/x",
		`C:`:           "/c",
		"/home/u":      "/home/u",
	} {
		if got := posixPath(in); got != want {
			t.Errorf("posixPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeGlob(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u/p":          "/home/u/p",
		"/home/u/my project": "/home/u/my project",
		"/home/u/[work]/app": `/home/u/\[work\]/app`,
		"/a/{x,y}/b*?!":      `/a/\{x,y\}/b\*\?\!`,
	} {
		if got := escapeGlob(in); got != want {
			t.Errorf("escapeGlob(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRulesEscapeGlobs(t *testing.T) {
	rules := funcs["rules"].(func(string, []string) []string)
	dirs := []string{"/Users/a/[work]/app"}
	if got := rules("Edit(/%s/**)", dirs); got[0] != `Edit(//Users/a/\[work\]/app/**)` {
		t.Errorf("Edit rule = %q", got[0])
	}
	if got := rules("Read(/%s/**)", dirs); got[0] != `Read(//Users/a/\[work\]/app/**)` {
		t.Errorf("Read rule = %q", got[0])
	}
	// A command rule is not a path glob.
	if got := rules("Bash(%s:*)", []string{"git [x]"}); got[0] != "Bash(git [x]:*)" {
		t.Errorf("Bash rule = %q", got[0])
	}
}
