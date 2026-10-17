+++
title   = "TUI and server gotchas"
touches = ["internal/tui/", "internal/server/", "internal/view/"]
source  = "T210"
+++
## Watch out
- Golden files: a change to the sidebar, popups or the status bar changes the goldens; update them with -update and read the diff.
- The project popup's tabs are drawn by mods; a failed mod shows its log, not a blank tab.
- Key handling: the prefix key is read before the pane gets input; a popup open over a session swallows keys until it closes.
- Rendering runs on the server; the console only paints frames.
