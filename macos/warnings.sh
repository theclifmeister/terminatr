#!/bin/bash
# Runs a swift build or test (macos/Makefile) and fails, after it, when
# it printed a warning from our own Sources/ or Tests/: SwiftTerm's, in
# .build/checkouts, and the build system's own aren't ours.
set -uo pipefail
log=$(mktemp "${TMPDIR:-/tmp}/tm-swift.XXXXXX")
trap 'rm -f "$log"' EXIT
dir=$(pwd)
"$@" 2>&1 | tee "$log"
status=$?
[ "$status" -eq 0 ] || exit "$status"
ours=$(sed 's/\x1b\[[0-9;]*m//g' "$log" | grep -F -e "$dir/Sources/" -e "$dir/Tests/" | grep -E '^[^ ]+\.swift:[0-9]+:[0-9]+: warning:' | sort -u)
if [ -n "$ours" ]; then
	echo "warnings.sh: our Swift warnings fail the build (WERROR=0 lets them by):" >&2
	echo "$ours" >&2
	exit 1
fi
