package modhost

import (
	"runtime"
	"runtime/debug"
	"runtime/metrics"
)

// The Go runtime settings of a host (T349, T360). A host runs one goja
// VM, which is single-threaded, and is usually idle, so it needs few OS
// threads and can trade GC CPU for a smaller heap.
const (
	// MaxProcs is the host's GOMAXPROCS: the VM's thread, and one for the
	// budget timer, the protocol's reader and the GC.
	MaxProcs = 2
	// GCPercent is the host's GOGC: half the default heap growth.
	GCPercent = 50
	// MemoryLimit is the host's soft memory limit (GOMEMLIMIT): above it
	// the GC works harder, well before the supervisor's RSS limit
	// (modsup.DefaultRSSLimit, 256 MiB) kills the host.
	MemoryLimit = 128 << 20
)

// Tune applies MaxProcs, GCPercent and MemoryLimit to this process,
// each unless the environment (getenv) sets its variable, GOMAXPROCS,
// GOGC or GOMEMLIMIT, which then wins as it does for any Go program.
func Tune(getenv func(string) string) {
	if getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(MaxProcs)
	}
	if getenv("GOGC") == "" {
		debug.SetGCPercent(GCPercent)
	}
	if getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(MemoryLimit)
	}
}

// runtimeSettings reads the process's GOMAXPROCS, GC percent and soft
// memory limit, for stats.
func runtimeSettings() (procs, gcPercent int, memLimit int64) {
	s := []metrics.Sample{{Name: "/gc/gogc:percent"}, {Name: "/gc/gomemlimit:bytes"}}
	metrics.Read(s)
	return runtime.GOMAXPROCS(0), int(s[0].Value.Uint64()), int64(s[1].Value.Uint64())
}
