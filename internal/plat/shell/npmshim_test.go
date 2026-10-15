package shell

import (
	"strings"
	"testing"
)

// npmShim is what npm 7 and later (cmd-shim) write for a package's bin.
const npmShim = "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n\r\n" +
	"IF EXIST \"%dp0%\\node.exe\" (\r\n  SET \"_prog=%dp0%\\node.exe\"\r\n) ELSE (\r\n  SET \"_prog=node\"\r\n  SET PATHEXT=%PATHEXT:;.JS;=;%\r\n)\r\n\r\n" +
	"endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & \"%_prog%\"  \"%dp0%\\node_modules\\@openai\\codex\\bin\\codex.js\" %*\r\n"

// oldNpmShim is what npm 6 wrote.
const oldNpmShim = "@IF EXIST \"%~dp0\\node.exe\" (\r\n  \"%~dp0\\node.exe\"  \"%~dp0\\..\\codex\\bin\\codex.js\" %*\r\n) ELSE (\r\n" +
	"  @SETLOCAL\r\n  @SET PATHEXT=%PATHEXT:;.JS;=;%\r\n  node  \"%~dp0\\..\\codex\\bin\\codex.js\" %*\r\n)"

func TestParseNodeShim(t *testing.T) {
	for _, c := range []struct {
		name, text, want string
	}{
		{"npm", npmShim, `node_modules\@openai\codex\bin\codex.js`},
		{"npm lf", strings.ReplaceAll(npmShim, "\r\n", "\n"), `node_modules\@openai\codex\bin\codex.js`},
		{"npm 6", oldNpmShim, `..\codex\bin\codex.js`},
		// A #! naming another interpreter: _prog isn't node.
		{"sh", strings.ReplaceAll(npmShim, `"_prog=node"`, `"_prog=sh"`), ""},
		// The interpreter takes arguments of its own.
		{"node flags", strings.Replace(npmShim, `"%_prog%"  "`, `"%_prog%" --x "`, 1), ""},
		{"two scripts", strings.Replace(oldNpmShim, `..\codex\bin\codex.js`, `..\other.js`, 1), ""},
		{"echo", "@echo off\r\n>\"%~dp0args\" echo(%*\r\n", ""},
		{"sh shim", "@\"C:\\Git\\bin\\sh.exe\" \"%~dpn0\" %*\r\n", ""},
		{"no args", strings.ReplaceAll(npmShim, " %*", ""), ""},
	} {
		got, ok := parseNodeShim(c.text)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s: %q %v, want %q", c.name, got, ok, c.want)
		}
	}
}
