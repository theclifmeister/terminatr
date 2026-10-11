package modsup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogMax is the size at which a mod's log is rotated: <id>.log moves to
// <id>.log.1, replacing the older one.
const LogMax = 1 << 20

// modLog is a mod's log, ~/.terminatr/logs/mods/<id>.log: its host's
// stderr, its $.log lines, its failed handlers and the supervisor's own
// lines. A log that can't be written is skipped: the mod still runs.
type modLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func openLog(path string) *modLog {
	l := &modLog{path: path}
	l.open()
	return l
}

func (l *modLog) open() {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	if fi, err := f.Stat(); err == nil {
		l.size = fi.Size()
	}
	l.f = f
}

// Printf writes one line, stamped.
func (l *modLog) Printf(format string, args ...any) {
	if l == nil {
		return
	}
	line := time.Now().Format("2006-01-02T15:04:05.000Z07:00") + " " +
		strings.TrimRight(fmt.Sprintf(format, args...), "\n") + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	// The broker appends its audit lines to the same file (whole lines,
	// O_APPEND), so the size is read, not only counted.
	if l.f != nil {
		if fi, err := l.f.Stat(); err == nil {
			l.size = fi.Size()
		}
	}
	if l.f != nil && l.size > 0 && l.size+int64(len(line)) > LogMax {
		l.f.Close()
		l.f = nil
		_ = os.Rename(l.path, l.path+".1")
		l.size = 0
		l.open()
	}
	if l.f == nil {
		return
	}
	n, _ := l.f.WriteString(line)
	l.size += int64(n)
}

func (l *modLog) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}
