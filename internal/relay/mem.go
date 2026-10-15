package relay

import (
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// connMemory is what one relay connection may take at its busiest: a
// message being read and one being written, the TLS and read buffers,
// two goroutines, and the GC's room on top.
const connMemory = 160 << 10

// MemoryLimit is the memory the relay should stay under: its soft limit
// (GOMEMLIMIT), or three quarters of its cgroup's hard one (systemd's
// MemoryMax, Docker's mem_limit) when that is lower; 0 for none known.
func MemoryLimit() int64 {
	limit := debug.SetMemoryLimit(-1)
	for _, f := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && n > 0 && n/4*3 < limit {
			limit = n / 4 * 3
		}
		break
	}
	if limit == math.MaxInt64 {
		return 0
	}
	return limit
}

// ConnsFor is how many connections fit in mem bytes, at least 16.
func ConnsFor(mem int64) int {
	return int(max(16, min(mem/connMemory, math.MaxInt32)))
}
