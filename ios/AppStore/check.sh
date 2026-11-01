#!/bin/bash
# Checks the App Store text against App Store Connect's limits (Apple's
# "App information" and "Platform version information" reference pages,
# checked 2026-11-01): characters for most fields, bytes for keywords
# and the review notes. The trailing newline doesn't count. make -C ios
# store-text runs it.
set -euo pipefail
cd "$(dirname "$0")"
export LC_ALL=en_US.UTF-8
bad=0
check() { # file limit chars|bytes
	local n
	if [ "$3" = bytes ]; then
		n=$(printf '%s' "$(cat "$1")" | wc -c)
	else
		n=$(printf '%s' "$(cat "$1")" | wc -m)
	fi
	n=$((n))
	if [ "$n" -gt "$2" ] || [ "$n" -eq 0 ]; then
		printf '%-34s %5d / %d %s  FAIL\n' "$1" "$n" "$2" "$3"
		bad=1
	else
		printf '%-34s %5d / %d %s\n' "$1" "$n" "$2" "$3"
	fi
}
for l in */name.txt; do
	l=${l%/name.txt}
	check "$l/name.txt" 30 chars
	check "$l/subtitle.txt" 30 chars
	check "$l/promotional_text.txt" 170 chars
	check "$l/keywords.txt" 100 bytes
	check "$l/description.txt" 4000 chars
	check "$l/release_notes.txt" 4000 chars
	if grep -q ', ' "$l/keywords.txt"; then
		echo "$l/keywords.txt: commas with no spaces"
		bad=1
	fi
done
check review_information/notes.txt 4000 bytes
exit $bad
