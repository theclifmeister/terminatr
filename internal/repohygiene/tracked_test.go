// Package repohygiene holds checks on what the repository tracks.
package repohygiene

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// executableMagic holds the leading bytes of native executables and shared
// libraries: ELF, PE, Mach-O (thin, both byte orders) and fat/universal.
var executableMagic = [][]byte{
	[]byte("\x7fELF"),
	[]byte("MZ"),
	{0xfe, 0xed, 0xfa, 0xce}, {0xfe, 0xed, 0xfa, 0xcf},
	{0xce, 0xfa, 0xed, 0xfe}, {0xcf, 0xfa, 0xed, 0xfe},
	{0xca, 0xfe, 0xba, 0xbe},
}

func isExecutable(head []byte) bool {
	for _, m := range executableMagic {
		if bytes.HasPrefix(head, m) {
			return true
		}
	}
	return false
}

func TestIsExecutable(t *testing.T) {
	for name, head := range map[string][]byte{
		"mach-o arm64": {0xcf, 0xfa, 0xed, 0xfe, 0x0c, 0x00, 0x00, 0x01},
		"elf":          []byte("\x7fELF\x02\x01"),
		"pe":           []byte("MZ\x90\x00"),
	} {
		if !isExecutable(head) {
			t.Errorf("%s not detected", name)
		}
	}
	for name, head := range map[string][]byte{
		"text":  []byte("package main\n"),
		"png":   {0x89, 'P', 'N', 'G'},
		"empty": nil,
	} {
		if isExecutable(head) {
			t.Errorf("%s flagged", name)
		}
	}
}

// TestNoTrackedExecutables fails when a committed file is a native binary
// (such as the output of a stray `go build` in a package folder): it bloats
// every clone for good and can carry machine-specific paths.
func TestNoTrackedExecutables(t *testing.T) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not in a git checkout: %v", err)
	}
	root := strings.TrimSpace(string(out))
	files, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	for _, name := range strings.Split(string(files), "\x00") {
		if name == "" {
			continue
		}
		f, err := os.Open(filepath.Join(root, name))
		if err != nil {
			continue // deleted in the working tree, or a submodule
		}
		head := make([]byte, 8)
		n, _ := f.Read(head)
		f.Close()
		if isExecutable(head[:n]) {
			t.Errorf("%s is a tracked native executable; remove it and build into bin/ or a temp dir", name)
		}
	}
}
