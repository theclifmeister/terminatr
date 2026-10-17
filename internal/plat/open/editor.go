package open

import (
	"os/exec"
	"runtime"
)

// Editor is the editor to run when $VISUAL and $EDITOR name none: the
// first of nano and vi on the PATH (notepad on Windows); nil for none.
func Editor() []string {
	fallback := []string{"nano", "vi"}
	if runtime.GOOS == "windows" {
		fallback = []string{"notepad"}
	}
	for _, name := range fallback {
		if _, err := exec.LookPath(name); err == nil {
			return []string{name}
		}
	}
	return nil
}
