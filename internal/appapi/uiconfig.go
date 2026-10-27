package appapi

import (
	"sync"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// needsFirst is the config's [ui] needs_first, as the TUI's sidebar
// reads it (tui configCache): config.toml loaded again only when its
// stat stamp changed, so a session.list costs a stat. A config that
// doesn't load says false, as in the TUI.
var needsFirst = func() func() bool {
	var mu sync.Mutex
	var stamp uint64
	loaded, on := false, false
	return func() bool {
		path, err := config.Path()
		if err != nil {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		if st := fsx.StatStamp(path); !loaded || st != stamp {
			loaded, stamp, on = true, st, false
			if cfg, err := config.Load(); err == nil {
				on = cfg.NeedsFirst
			}
		}
		return on
	}
}()
