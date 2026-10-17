+++
title   = "Platform research: libghostty, Zig, Windows ConPTY"
touches = ["third_party/ghostty/", "build.zig"]
source  = "T218"
+++
## Decided
- libghostty builds with the pinned Zig (0.15.2); bumping the Zig pin needs a rebuild of the cached snapshot on every platform.
- Windows uses ConPTY through OpenConsole.exe shipped next to tm.exe.

## Ruled out
- winpty: unmaintained, breaks colours.

## Watch out
- A Zig bump changes aarch64 codegen: run the Linux and Windows smoke tests before merging.
