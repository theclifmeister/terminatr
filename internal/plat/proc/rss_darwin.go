package proc

import (
	"bufio"
	"bytes"
	"os/exec"
	"strconv"
	"strings"
)

// rss asks ps, once for every pid: macOS gives another process's
// resident set only through libproc (proc_pid_rusage), which Go can't
// call without cgo.
func rss(pids []int, out map[int]uint64) error {
	var list []string
	for _, p := range pids {
		// ps fails as a whole on a pid out of range.
		if Alive(p) {
			list = append(list, strconv.Itoa(p))
		}
	}
	if len(list) == 0 {
		return nil
	}
	b, err := exec.Command("/bin/ps", "-o", "pid=,rss=", "-p", strings.Join(list, ",")).Output()
	if err != nil && len(b) == 0 {
		// ps exits 1 when none of the pids is alive.
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		kb, err2 := strconv.ParseUint(f[1], 10, 64)
		if err1 == nil && err2 == nil {
			out[pid] = kb << 10
		}
	}
	return nil
}
