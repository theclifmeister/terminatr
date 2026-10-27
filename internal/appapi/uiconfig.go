package appapi

import (
	"sync"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// uiHints are the config's [ui] needs_first and context_hint, as the
// TUI's sidebar reads them (tui configCache): config.toml loaded again
// only when its stat stamp changed, so a session.list costs a stat. A
// config that doesn't load says false and the default hint, as in the
// TUI.
var uiHints = func() func() (needsFirst bool, ctxHint int) {
	var mu sync.Mutex
	var stamp uint64
	loaded, on, hint := false, false, config.DefaultContextHint
	return func() (bool, int) {
		path, err := config.Path()
		if err != nil {
			return false, config.DefaultContextHint
		}
		mu.Lock()
		defer mu.Unlock()
		if st := fsx.StatStamp(path); !loaded || st != stamp {
			loaded, stamp, on, hint = true, st, false, config.DefaultContextHint
			if cfg, err := config.Load(); err == nil {
				on, hint = cfg.NeedsFirst, cfg.ContextHint
			}
		}
		return on, hint
	}
}()
