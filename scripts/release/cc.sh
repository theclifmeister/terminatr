#!/bin/sh
# The C compiler for release builds: zig cc for $TM_ZIG_TARGET, with the
# Zig that scripts/release/ghostty.sh linked into .build/release/zig
# ($TM_ZIG overrides).
# goreleaser sets CC to this script and TM_ZIG_TARGET per target.
#
# macOS targets use Apple's clang and ld instead, from the Command Line
# Tools (the release workflow runs on macos-latest). Zig's linker writes
# old-style rebase info (LC_DYLD_INFO_ONLY): dyld then rewrites pointers
# in __DATA_CONST at every launch, leaving ~2.4 MB dirty in every tm
# process. Apple's ld emits chained fixups (LC_DYLD_CHAINED_FIXUPS) and
# those pages stay clean. The deployment target stays at Zig's default,
# macOS 13.0 (scripts/release/check.sh checks both).
root=$(cd "$(dirname "$0")/../.." && pwd)
target=${TM_ZIG_TARGET:?TM_ZIG_TARGET is not set}
macos_min=13.0
case $target in
aarch64-macos*) exec xcrun --sdk macosx clang -target "arm64-apple-macos$macos_min" "$@" ;;
x86_64-macos*) exec xcrun --sdk macosx clang -target "x86_64-apple-macos$macos_min" "$@" ;;
esac
zig=${TM_ZIG:-$root/.build/release/zig}
# Go probes the compiler with `-### -x c -c -`, which zig cc runs for real,
# leaving a "-.o" in the working directory: probe in a scratch directory.
if [ "${1:-}" = "-###" ]; then
	tmp=$(mktemp -d) || exit 1
	(cd "$tmp" && "$zig" cc -target "$target" "$@")
	rc=$?
	rm -rf "$tmp"
	exit $rc
fi
exec "$zig" cc -target "$target" "$@"
