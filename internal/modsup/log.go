package modsup

import (
	"fmt"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/logrot"
)

// LogMax is the size at which a mod's log is rotated: <id>.log moves to
// <id>.log.1, replacing the older one.
const LogMax = 1 << 20

// modLog is a mod's log, ~/.terminatr/logs/mods/<id>.log: its host's
// stderr, its $.log lines, its failed handlers and the supervisor's own
// lines. The broker appends its audit lines to the same file. A log that
// can't be written is skipped: the mod still runs.
type modLog struct{ f *logrot.File }

func openLog(path string) *modLog {
	f, _ := logrot.Open(path, LogMax, 1)
	return &modLog{f}
}

// Printf writes one line, stamped.
func (l *modLog) Printf(format string, args ...any) {
	if l == nil {
		return
	}
	line := time.Now().Format("2006-01-02T15:04:05.000Z07:00") + " " +
		strings.TrimRight(fmt.Sprintf(format, args...), "\n") + "\n"
	l.f.Write([]byte(line))
}

func (l *modLog) Close() {
	if l == nil {
		return
	}
	l.f.Close()
}
