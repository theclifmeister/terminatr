package proc

import (
	"bytes"
	"os"
	"strconv"
)

// rss reads the resident pages from /proc/<pid>/statm.
func rss(pids []int, out map[int]uint64) error {
	page := uint64(os.Getpagesize())
	for _, pid := range pids {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
		if err != nil {
			continue
		}
		f := bytes.Fields(b)
		if len(f) < 2 {
			continue
		}
		if n, err := strconv.ParseUint(string(f[1]), 10, 64); err == nil {
			out[pid] = n * page
		}
	}
	return nil
}
