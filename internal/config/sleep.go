package config

import (
	"fmt"
	"strings"
	"time"
)

// Coordinator sleep (docs/SPEC.md §7.5, Sleep): a coordinator idle for
// coordinator_sleep_after, with nothing waiting on it and nobody
// watching, is stopped with its conversation kept, and resumed on
// demand.

const (
	// SleepOff is coordinator_sleep_after's never; "0" says it too.
	SleepOff = "off"
	// ProgramSleepAfter is a program member's coordinator_sleep_after
	// when no table sets it; a standalone project's never sleeps.
	ProgramSleepAfter = 4 * time.Hour
	// MinSleepAfter and MaxSleepAfter bound a duration.
	MinSleepAfter = 5 * time.Minute
	MaxSleepAfter = 7 * 24 * time.Hour
)

// CheckSleepAfter checks a coordinator_sleep_after value: "off", "0",
// or a duration of MinSleepAfter to MaxSleepAfter ("30m", "4h").
func CheckSleepAfter(v string) error {
	_, err := parseSleep(v)
	return err
}

func parseSleep(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == SleepOff || v == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < MinSleepAfter || d > MaxSleepAfter {
		return 0, limit(fmt.Sprintf("must be %q, \"0\" or a duration of %v to %v (as \"4h\")", SleepOff, MinSleepAfter, MaxSleepAfter))
	}
	return d, nil
}

// SleepAfter is how long s's coordinator may idle before it sleeps, 0
// for never: its coordinator_sleep_after, else ProgramSleepAfter when
// the project is a member of a running program (inProgram), else never.
func SleepAfter(s Safety, inProgram bool) time.Duration {
	if s.CoordinatorSleepAfter == "" {
		if inProgram {
			return ProgramSleepAfter
		}
		return 0
	}
	d, err := parseSleep(s.CoordinatorSleepAfter)
	if err != nil {
		return 0
	}
	return d
}
