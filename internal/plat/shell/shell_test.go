package shell

import (
	"errors"
	"slices"
	"testing"
)

// kinds are every kind, in a stable order.
var kinds = []kind{kindSh, kindBash, kindZsh, kindPwsh, kindPowerShell, kindCmd}

func TestInteractive(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	path := func(found ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, f := range found {
				if f == name {
					return `C:\bin\` + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	for _, c := range []struct {
		name  string
		goos  string
		env   map[string]string
		found []string
		want  []string
	}{
		{"shell", "darwin", map[string]string{"SHELL": "/bin/zsh"}, nil, []string{"/bin/zsh", "-l"}},
		{"no shell", "linux", nil, nil, []string{"/bin/sh", "-l"}},
		{"pwsh", "windows", map[string]string{"COMSPEC": `C:\cmd.exe`}, []string{"pwsh.exe", "powershell.exe"}, []string{`C:\bin\pwsh.exe`, "-NoLogo"}},
		{"powershell", "windows", nil, []string{"powershell.exe"}, []string{`C:\bin\powershell.exe`, "-NoLogo"}},
		{"comspec", "windows", map[string]string{"COMSPEC": `C:\Windows\system32\cmd.exe`}, nil, []string{`C:\Windows\system32\cmd.exe`}},
		{"cmd", "windows", nil, nil, []string{"cmd.exe"}},
	} {
		if got := interactive(c.goos, env(c.env), path(c.found...)); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestQuote(t *testing.T) {
	in := []string{"", "a b", "it's", `say "hi"`, "100%", "x’y"}
	want := map[kind][]string{
		kindSh:         {`''`, `'a b'`, `'it'\''s'`, `'say "hi"'`, `'100%'`, `'x’y'`},
		kindBash:       {`''`, `'a b'`, `'it'\''s'`, `'say "hi"'`, `'100%'`, `'x’y'`},
		kindZsh:        {`''`, `'a b'`, `'it'\''s'`, `'say "hi"'`, `'100%'`, `'x’y'`},
		kindPwsh:       {`''`, `'a b'`, `'it''s'`, `'say "hi"'`, `'100%'`, `'x’’y'`},
		kindPowerShell: {`''`, `'a b'`, `'it''s'`, `'say "hi"'`, `'100%'`, `'x’’y'`},
		kindCmd:        {`""`, `"a b"`, `"it's"`, `"say ""hi"""`, `"100%"`, `"x’y"`},
	}
	for _, k := range kinds {
		for i, s := range in {
			if got := quote(k, s); got != want[k][i] {
				t.Errorf("quote(%s, %q) = %s, want %s", k, s, got, want[k][i])
			}
		}
	}
}

func TestCommand(t *testing.T) {
	type c struct {
		goos, exe string
		want      map[kind]string
	}
	all := func(s string) map[kind]string {
		m := map[kind]string{}
		for _, k := range kinds {
			m[k] = s
		}
		return m
	}
	args := []string{"hook", "--agent", "claude"}
	for _, c := range []c{
		{"darwin", "/usr/local/bin/tm", all("/usr/local/bin/tm hook --agent claude")},
		{"linux", "/home/me/my tools/tm", map[kind]string{
			kindSh:         `'/home/me/my tools/tm' hook --agent claude`,
			kindBash:       `'/home/me/my tools/tm' hook --agent claude`,
			kindZsh:        `'/home/me/my tools/tm' hook --agent claude`,
			kindPwsh:       `& '/home/me/my tools/tm' hook --agent claude`,
			kindPowerShell: `& '/home/me/my tools/tm' hook --agent claude`,
			kindCmd:        `"/home/me/my tools/tm" hook --agent claude`,
		}},
		// herdr t-0012: an unquoted forward-slash exe runs in every shell.
		{"windows", `C:\Users\me\.terminatr\bin\tm.exe`, all("C:/Users/me/.terminatr/bin/tm.exe hook --agent claude")},
		// The 8.3 form of a folder with a space (ShortPath) stays bare.
		{"windows", `C:\PROGRA~1\tm\tm.exe`, all("C:/PROGRA~1/tm/tm.exe hook --agent claude")},
		// Without short names: quoted, and & in PowerShell.
		{"windows", `C:\Program Files\tm\tm.exe`, map[kind]string{
			kindSh:         `'C:/Program Files/tm/tm.exe' hook --agent claude`,
			kindBash:       `'C:/Program Files/tm/tm.exe' hook --agent claude`,
			kindZsh:        `'C:/Program Files/tm/tm.exe' hook --agent claude`,
			kindPwsh:       `& 'C:/Program Files/tm/tm.exe' hook --agent claude`,
			kindPowerShell: `& 'C:/Program Files/tm/tm.exe' hook --agent claude`,
			kindCmd:        `"C:/Program Files/tm/tm.exe" hook --agent claude`,
		}},
		// The exe an environment variable names (Codex's hook).
		{"linux", "$TERMINATR_BIN", map[kind]string{
			kindSh:         `"$TERMINATR_BIN" hook --agent claude`,
			kindBash:       `"$TERMINATR_BIN" hook --agent claude`,
			kindZsh:        `"$TERMINATR_BIN" hook --agent claude`,
			kindPwsh:       `& $env:TERMINATR_BIN hook --agent claude`,
			kindPowerShell: `& $env:TERMINATR_BIN hook --agent claude`,
			kindCmd:        `"%TERMINATR_BIN%" hook --agent claude`,
		}},
	} {
		for _, k := range kinds {
			if got := command(k, c.goos, c.exe, args...); got != c.want[k] {
				t.Errorf("command(%s, %s, %q) = %s, want %s", k, c.goos, c.exe, got, c.want[k])
			}
		}
	}
	// Arguments are quoted only when they need it; backslashes in them stay.
	for k, want := range map[kind]string{
		kindSh:   `tm run 'a b' '' 'C:\x'`,
		kindPwsh: `tm run 'a b' '' 'C:\x'`,
		kindCmd:  `tm run "a b" "" "C:\x"`,
	} {
		if got := command(k, "windows", "tm", "run", "a b", "", `C:\x`); got != want {
			t.Errorf("command(%s) args = %s, want %s", k, got, want)
		}
	}
}

func TestUnwrap(t *testing.T) {
	for _, c := range []struct{ in, want []string }{
		{[]string{"claude", "--resume", "x"}, []string{"claude", "--resume", "x"}},
		{[]string{"/usr/bin/node", "--no-warnings", "/usr/lib/bin/claude", "-c"}, []string{"/usr/lib/bin/claude", "-c"}},
		{[]string{"deno", "run", "-A", "x.ts"}, []string{"-A", "x.ts"}},
		{[]string{"deno", "-q", "run", "x.ts"}, []string{"x.ts"}},
		{[]string{"/bin/sh", "-c", "exec claude --model x"}, []string{"claude", "--model", "x"}},
		{[]string{"bash", "-c", `claude --append 'a b' "c d"`}, []string{"claude", "--append", "a b", "c d"}},
		{[]string{"/bin/zsh", "-l"}, []string{"/bin/zsh", "-l"}},
		{[]string{"node"}, []string{"node"}},
		{[]string{"sh", "-c", ""}, []string{"sh", "-c", ""}},
		{nil, nil},

		// Windows names: either separator, any case, .exe.
		{[]string{`C:\Program Files\nodejs\NODE.EXE`, `C:\npm\claude\cli.js`}, []string{`C:\npm\claude\cli.js`}},
		{[]string{`C:\Users\me\.local\bin\claude.exe`, "--resume"}, []string{`C:\Users\me\.local\bin\claude`, "--resume"}},
		{[]string{`C:/Git/usr/bin/bash.exe`, "-c", "codex"}, []string{"codex"}},

		// cmd /c
		{[]string{`C:\Windows\system32\cmd.exe`, "/d", "/s", "/c", `"claude.exe --resume x"`}, []string{"claude", "--resume", "x"}},
		{[]string{"cmd", "/C", "codex", "exec"}, []string{"codex", "exec"}},
		{[]string{"cmd.exe", "/k", "claude"}, []string{"claude"}},
		{[]string{"cmd.exe"}, []string{"cmd"}},
		{[]string{"cmd.exe", "/q"}, []string{"cmd", "/q"}},

		// pwsh / powershell
		{[]string{"pwsh.exe", "-NoLogo", "-NoProfile", "-command", "& 'C:\\x y\\claude.exe' --resume"}, []string{`C:\x y\claude`, "--resume"}},
		{[]string{"pwsh", "-c", "claude", "-p"}, []string{"claude", "-p"}},
		{[]string{"PowerShell.exe", "-ExecutionPolicy", "Bypass", "-File", `C:\x\claude.ps1`, "-p"}, []string{`C:\x\claude.ps1`, "-p"}},
		{[]string{"pwsh", "-nop", `C:\x\run.ps1`, "a"}, []string{`C:\x\run.ps1`, "a"}},
		{[]string{"powershell", "-NoProfile", "codex", "exec"}, []string{"codex", "exec"}},
		{[]string{"pwsh", "-NoLogo"}, []string{"pwsh", "-NoLogo"}},
		{[]string{"pwsh.exe", "-wd", `C:\x`, "-command", "node cli.js"}, []string{"cli.js"}},

		// Nested: cmd running pwsh running claude.
		{[]string{"cmd", "/c", "pwsh -NoProfile -command claude"}, []string{"claude"}},
	} {
		if got := Unwrap(c.in); !slices.Equal(got, c.want) {
			t.Errorf("Unwrap(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplit(t *testing.T) {
	for in, want := range map[string][]string{
		"":                  nil,
		"  a  b\t c ":       {"a", "b", "c"},
		`a "b c" 'd e'`:     {"a", "b c", "d e"},
		`a"b c"d`:           {"ab cd"},
		`'it"s' ""`:         {`it"s`, ""},
		"exec claude --x=1": {"exec", "claude", "--x=1"},
	} {
		if got := split(in); !slices.Equal(got, want) {
			t.Errorf("split(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestHookShell: Unix hooks run in sh, Windows ones in PowerShell.
func TestHookShell(t *testing.T) {
	for goos, want := range map[string]string{
		"linux":   `"$TERMINATR_BIN" hook --agent codex`,
		"darwin":  `"$TERMINATR_BIN" hook --agent codex`,
		"windows": `& $env:TERMINATR_BIN hook --agent codex`,
	} {
		k, g := hookShell(goos)
		if got := command(k, g, "$TERMINATR_BIN", "hook", "--agent", "codex"); got != want {
			t.Errorf("%s: %s, want %s", goos, got, want)
		}
	}
}
