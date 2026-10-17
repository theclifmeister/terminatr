package tui

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/logrot"
)

// The console's own log, logs/console.log: what went wrong on the
// client's side (an error the footer showed, a view call that failed, a
// panic), which the server's log never sees. Every console of this home
// appends to it, each line marked with its pid. It holds no keys, no
// pane output and no prompts: the attach client's diagnostics, keys
// among them, stay in TERMINATR_ATTACH_LOG, off by default. The Logs
// tab of the , popup shows it (logstab.go).
const (
	consoleLogMax  = 1 << 20
	consoleLogKeep = 1
)

// ConsoleLogName is its file name in logs/.
const ConsoleLogName = "console.log"

var consoleLog = sync.OnceValue(func() *log.Logger {
	w := io.Discard
	if dir, err := home.LogsDir(); err == nil {
		// A file that can't be opened drops the lines: the console runs on.
		f, _ := logrot.Open(filepath.Join(dir, ConsoleLogName), consoleLogMax, consoleLogKeep)
		w = f
	}
	return log.New(w, fmt.Sprintf("pid %d: ", os.Getpid()), log.LstdFlags)
})

// clog adds an error to the console's log.
func clog(format string, args ...any) { consoleLog().Printf("error: "+format, args...) }
