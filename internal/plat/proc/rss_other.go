//go:build !darwin && !linux && !windows

package proc

import "errors"

func rss(pids []int, out map[int]uint64) error { return errors.ErrUnsupported }
