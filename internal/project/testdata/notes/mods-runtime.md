+++
title   = "Mods runtime: goja hosts, limits, sockets"
touches = ["internal/modhost/", "internal/modbroker/"]
source  = "T330"
+++
## Decided
- Each code mod runs in its own goja host process; the supervisor kills a host past 256 MiB resident memory.
- Hosts talk to the server over mod.sock; every call is audited by the broker.

## Ruled out
- Running mods in the server process: one bad mod would take the server down.

## Watch out
- goja has no heap cap: a runaway mod is only stopped by the RSS check, every 2 s.
