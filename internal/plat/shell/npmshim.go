package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// npmShimRun matches the line of an npm shim (cmd-shim's .cmd) that runs
// node on the package's script: `"%_prog%"  "%dp0%\<script>" %*`, or in
// shims from before npm 7 `"%~dp0\node.exe"  "%~dp0\<script>" %*` and
// `node  "%~dp0\<script>" %*`. A shim whose interpreter takes arguments
// of its own doesn't match.
var npmShimRun = regexp.MustCompile(`(?m)(?:"%_prog%"|"%~dp0\\node\.exe"|(?:^|\s)node)[ \t]+"%(?:dp0%|~dp0)\\([^"%\r\n]+)"[ \t]+%\*[ \t]*\r?$`)

// parseNodeShim returns the script, relative to the shim's folder, that
// the npm shim text runs with node, if it is one. In the current form the
// program is %_prog%, which must then be node (cmd-shim writes another
// interpreter there for a script whose #! names one).
func parseNodeShim(text string) (string, bool) {
	ms := npmShimRun.FindAllStringSubmatch(text, -1)
	if len(ms) == 0 {
		return "", false
	}
	script := ms[0][1]
	for _, m := range ms[1:] {
		if m[1] != script {
			return "", false
		}
	}
	if strings.Contains(ms[0][0], "%_prog%") &&
		(!strings.Contains(text, `SET "_prog=node"`) || !strings.Contains(text, `SET "_prog=%dp0%\node.exe"`)) {
		return "", false
	}
	return script, true
}

// maxShim bounds what NodeShim reads: npm's shims are well under 1 KiB.
const maxShim = 16 << 10

// NodeShim reports whether the batch file at path is an npm shim that runs
// a node script, and if so the node.exe it would run (the one beside it,
// else node.exe on PATH) and the script, so the script can be started
// as node <script> args without cmd.exe in between.
func NodeShim(path string) (node, script string, ok bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() > maxShim {
		return "", "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	rel, ok := parseNodeShim(string(b))
	if !ok {
		return "", "", false
	}
	dir := filepath.Dir(path)
	script = filepath.Join(dir, rel)
	if fi, err := os.Stat(script); err != nil || fi.IsDir() {
		return "", "", false
	}
	node = filepath.Join(dir, "node.exe")
	if fi, err := os.Stat(node); err != nil || fi.IsDir() {
		// The shim drops .JS from PATHEXT so node can't be node.js; an
		// explicit .exe can't be a batch file either.
		if node, err = exec.LookPath("node.exe"); err != nil {
			return "", "", false
		}
	}
	return node, script, true
}

// IsBatch reports whether path, as LookPath found it, is a batch file,
// which CreateProcess runs through cmd.exe.
func IsBatch(path string) bool {
	low := strings.ToLower(path)
	return strings.HasSuffix(low, ".cmd") || strings.HasSuffix(low, ".bat")
}
