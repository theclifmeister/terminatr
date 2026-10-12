//go:build !darwin && !linux && !windows

package sandbox

import (
	"fmt"
	"os/exec"
	"runtime"
)

const mode = "none"

func check() error { return fmt.Errorf("%w on %s", ErrUnavailable, runtime.GOOS) }

func start(*exec.Cmd, Policy) (*Proc, error) { return nil, check() }

func lockdown() error { return nil }
