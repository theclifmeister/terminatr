package agent

import (
	"os"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestThreadTempLaunch (T587): a thread's own temp folder is Claude's
// CLAUDE_CODE_TMPDIR and TMPDIR and Codex's TMPDIR; its NoRead folders
// are Claude's sandbox denyRead with its own folder the allowRead, and
// Read and Edit deny rules for those that don't hold it; Codex's none,
// its own write. Without one, no variable is set and no rule written.
func TestThreadTempLaunch(t *testing.T) {
	spec := threadSpec()
	spec.TempDir = "/tmp/tm-1/abc"
	spec.Access.OwnTemp = spec.TempDir
	spec.Access.NoRead = []string{"/tmp/tm-1", "/tmp/claude-1"}
	l, err := claude(t).Launch(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range []string{"CLAUDE_CODE_TMPDIR=/tmp/tm-1/abc", "TMPDIR=/tmp/tm-1/abc"} {
		if !contains(l.Env, kv) {
			t.Errorf("env %q lacks %s", l.Env, kv)
		}
	}
	s := parseSettings(t, l.Files["claude-settings.json"])
	if f := s.Sandbox.Filesystem; f == nil || !reflect.DeepEqual(f.DenyRead, spec.Access.NoRead) || !reflect.DeepEqual(f.AllowRead, []string{spec.TempDir}) {
		t.Errorf("sandbox filesystem = %+v, want denyRead %q allowRead [%s]", f, spec.Access.NoRead, spec.TempDir)
	}
	for _, r := range []string{"Read(//tmp/claude-1/**)", "Edit(//tmp/claude-1/**)"} {
		if !slices.Contains(s.Permissions.Deny, r) {
			t.Errorf("deny %q lacks %s", s.Permissions.Deny, r)
		}
	}
	for _, r := range s.Permissions.Deny {
		if strings.Contains(r, "/tmp/tm-1/") {
			t.Errorf("deny rule %s takes the thread's own temp folder with it", r)
		}
	}

	reg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := reg.Get("codex")
	if !ok {
		t.Fatal("codex manifest not embedded")
	}
	cs := spec
	cs.BriefPath = "" // Codex reads it in
	lc, err := c.Launch(cs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(lc.Env, "TMPDIR=/tmp/tm-1/abc") {
		t.Errorf("codex env %q lacks TMPDIR", lc.Env)
	}
	argv := strings.Join(lc.Argv, " ")
	for _, m := range []string{`"/tmp/tm-1"="none"`, `"/tmp/claude-1"="none"`, `"/tmp/tm-1/abc"="write"`} {
		if !strings.Contains(argv, m) {
			t.Errorf("codex profile lacks %s: %s", m, argv)
		}
	}

	bare := threadSpec()
	l, err = claude(t).Launch(bare)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range l.Env {
		if strings.HasPrefix(kv, "TMPDIR=") || strings.HasPrefix(kv, "CLAUDE_CODE_TMPDIR=") {
			t.Errorf("no temp folder, yet %s", kv)
		}
	}
	if s := parseSettings(t, l.Files["claude-settings.json"]); s.Sandbox.Filesystem != nil {
		t.Errorf("no NoRead, yet a sandbox filesystem %+v", s.Sandbox.Filesystem)
	}
}

// TestPathModesNoRead: NoRead is none, winning over any grant, and
// OwnTemp inside it write.
func TestPathModesNoRead(t *testing.T) {
	a := Access{Read: []string{"/t"}, NoWrite: []string{"/c"}, NoRead: []string{"/t", "/c"}, OwnTemp: "/t/own"}
	if got, want := pathModes(a), `{"/t"="none","/c"="none","/t/own"="write"}`; got != want {
		t.Errorf("pathModes = %s, want %s", got, want)
	}
	if got, want := a.Closed(), []string{"/c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Closed = %q, want %q", got, want)
	}
}

// TestSharedTemps: Claude's per-user temp folder, which its sessions
// share, under $CLAUDE_CODE_TMPDIR too; never Codex's /tmp.
func TestSharedTemps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no uids")
	}
	uid := strconv.Itoa(os.Getuid())
	env := map[string]string{"CLAUDE_CODE_TMPDIR": "/x", "TMPDIR": "/y"}
	got := SharedTemps(nil, "/h", func(k string) string { return env[k] })
	if want := []string{"/x/claude-" + uid, "/tmp/claude-" + uid}; !reflect.DeepEqual(got, want) {
		t.Errorf("SharedTemps = %q, want %q", got, want)
	}
}

// TestOwnTempFits: Claude keeps its sandbox's TMPDIR in a per-uid
// folder of at most 44 bytes; Codex has no limit.
func TestOwnTempFits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no uids")
	}
	if !OwnTempFits(nil, "claude", "/private/tmp/tm-501/0123456789") {
		t.Error("a short folder doesn't fit")
	}
	if OwnTempFits(nil, "claude", "/Users/someone/.terminatr/tmp/project/t-0001") {
		t.Error("a long folder fits")
	}
	if !OwnTempFits(nil, "codex", "/Users/someone/.terminatr/tmp/project/t-0001") {
		t.Error("codex has a limit")
	}
}
