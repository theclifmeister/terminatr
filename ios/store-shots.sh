#!/bin/bash
# The App Store's screenshots (T911), from the demo (fictional names
# only): make store-shots runs it after make build. For each set it
# makes its own Simulator (deleted after), sets light appearance and
# the 9:41 status bar, launches the demo on each screen and shoots it,
# then checks every file's size against the set's and writes it without
# alpha (App Store Connect refuses alpha) to OUT/<set>/NN-name.png, with
# OUT/index.html to look them over.
#
#   store-shots.sh APP OUT [SET...]   (default: every set)
#
# Sets: iphone-medium 1206x2622 (iPhone 17; required), iphone-large
# 1320x2868 (iPhone 17 Pro Max), ipad-13 2064x2752 (iPad Pro 13-inch
# (M5); required), duo-outer 1398x2034 and duo-inner 2007x2853 (iPhone
# Duo; required from April 2027). Every set is portrait: simctl can't
# turn a device. Nor can it open a Duo (powering its inner display on
# leaves it black), so duo-inner isn't in the default sets: open a Duo
# Simulator in Xcode's Device Hub and pass its UDID as DUO, which it
# shoots and leaves as it was; duo-outer shoots a new Duo, closed.
set -euo pipefail

APP=${1:?app}
OUT=${2:?out}
shift 2
BUNDLE=dev.terminatr.ios
SETTLE=${SETTLE:-6}

# The demo's times move to now (DemoTransport), the clock in its
# conversations with them: launched in the whole-hour zone where now is
# nearest 10:00, they read within half an hour of 9:02 to 9:41 (as
# recorded) on any run, under the status
# bar's 9:41 (an ISO time, with milliseconds or simctl refuses it, in
# the Mac's zone; an iPad shows its date too).
m=$((10#$(date -u +%H) * 60 + 10#$(date -u +%M)))
off=$(((600 - m + 30 + 24 * 60) / 60 % 24))
[ "$off" -le 14 ] || off=$((off - 24))
if [ "$off" -ge 0 ]; then TZ_DEMO=Etc/GMT-$off; else TZ_DEMO=Etc/GMT+$((-off)); fi
z=$(date +%z)
CLOCK=2026-11-10T09:41:00.000${z:0:3}:${z:3:2}

# set: device type, size, display, screens.
type_of() {
	case $1 in
	iphone-medium) echo iPhone-17 ;;
	iphone-large) echo iPhone-17-Pro-Max ;;
	ipad-13) echo iPad-Pro-13-inch-M5-12GB ;;
	duo-outer | duo-inner) echo iPhone-Duo ;;
	*) return 1 ;;
	esac
}
size_of() {
	case $1 in
	iphone-medium) echo 1206x2622 ;;
	iphone-large) echo 1320x2868 ;;
	ipad-13) echo 2064x2752 ;;
	duo-outer) echo 1398x2034 ;;
	duo-inner) echo 2007x2853 ;;
	esac
}
display_of() {
	case $1 in
	duo-outer) echo primary ;;
	duo-inner) echo primary-1 ;;
	*) echo internal ;;
	esac
}

# The screens, in the store's order: name, then the launch arguments
# after -demo YES (Route.parse; TerminatrApp.swift).
SCREENS=(
	"needs-you|"
	"answer|-open|needs need:acme-web/t-0102"
	"conversation|-open|session:acme-web/t-0101"
	"terminal|-terminal|YES|-open|session:acme-web/t-0101"
	"project|-open|project:acme-web"
	"board|-open|board:acme-web"
	"board|-sidebar|NO|-open|board:acme-web"
)
# Each set's, by index. The Duo's outer display is small: Needs you,
# the answer and the terminal. An iPad in portrait shows Needs you's
# list beside an empty detail, a session across the whole width, and
# the sidebar it opens with over the list: the project with it, the
# board (beside the project) without.
screens_of() {
	case $1 in
	duo-outer) echo 0 1 3 ;;
	ipad-13) echo 1 2 3 4 6 ;;
	*) echo 0 1 2 3 4 5 ;;
	esac
}

SETS=("$@")
[ ${#SETS[@]} -gt 0 ] || SETS=(iphone-medium iphone-large ipad-13 duo-outer)

DEVICES=()
cleanup() {
	for u in "${DEVICES[@]:-}"; do
		[ -n "$u" ] || continue
		xcrun simctl shutdown "$u" 2>/dev/null || true
		xcrun simctl delete "$u" 2>/dev/null || true
	done
}
trap cleanup EXIT

shoot_set() {
	local set=$1 type size display dir u w h
	type=$(type_of "$set") || { echo "store-shots: no set $set" >&2; return 1; }
	size=$(size_of "$set")
	display=$(display_of "$set")
	dir=$OUT/$set
	rm -rf "$dir" && mkdir -p "$dir"
	if [ "$set" = duo-inner ]; then
		u=${DUO:?"store-shots: duo-inner shoots an opened iPhone Duo: DUO=its UDID"}
	else
		u=$(xcrun simctl create "store-shots $set" "com.apple.CoreSimulator.SimDeviceType.$type")
		DEVICES+=("$u")
		xcrun simctl boot "$u"
	fi
	echo "== $set: $type $size ($u)"
	xcrun simctl bootstatus "$u" >/dev/null
	xcrun simctl ui "$u" appearance light
	# The keyboard's swipe-typing intro would cover a composer.
	xcrun simctl spawn "$u" defaults write com.apple.Preferences DidShowContinuousPathIntroduction -bool true
	xcrun simctl status_bar "$u" override --time "$CLOCK" --dataNetwork wifi --wifiMode active --wifiBars 3 \
		--cellularMode active --cellularBars 4 --batteryState charged --batteryLevel 100
	# A Simulator just booted can refuse an install ("Failed to create
	# promise"): it takes a second try.
	local try
	for try in 1 2 3 4; do
		xcrun simctl install "$u" "$APP" && break
		[ "$try" -lt 4 ] || return 1
		sleep 5
	done
	local n=0 i
	for i in $(screens_of "$set"); do
		n=$((n + 1))
		local spec=${SCREENS[$i]} name args=()
		IFS='|' read -r -a args <<<"$spec"
		name=${args[0]}
		args=("${args[@]:1}")
		local f
		f=$(printf '%s/%02d-%s.png' "$dir" "$n" "$name")
		xcrun simctl terminate "$u" "$BUNDLE" 2>/dev/null || true
		SIMCTL_CHILD_TZ=$TZ_DEMO xcrun simctl launch "$u" "$BUNDLE" -demo YES ${args[@]+"${args[@]}"} >/dev/null
		sleep "$SETTLE"
		xcrun simctl io "$u" screenshot --type=png --display="$display" "$f.raw.png" >/dev/null 2>&1
		# Through JPEG at full quality and back: PNG without alpha.
		sips -s format jpeg -s formatOptions 100 "$f.raw.png" --out "$f.jpg" >/dev/null
		sips -s format png "$f.jpg" --out "$f" >/dev/null
		rm -f "$f.raw.png" "$f.jpg"
		w=$(sips -g pixelWidth "$f" | awk '/pixelWidth/ {print $2}')
		h=$(sips -g pixelHeight "$f" | awk '/pixelHeight/ {print $2}')
		if [ "${w}x${h}" != "$size" ]; then
			echo "store-shots: $f is ${w}x${h}, not $size" >&2
			return 1
		fi
		if sips -g hasAlpha "$f" | grep -q 'hasAlpha: yes'; then
			echo "store-shots: $f has alpha" >&2
			return 1
		fi
		echo "   $(basename "$f") ${w}x${h}"
	done
	[ "$set" = duo-inner ] || xcrun simctl shutdown "$u"
}

mkdir -p "$OUT"
for s in "${SETS[@]}"; do shoot_set "$s"; done

# The contact sheet: every set that is there, in a row each.
{
	echo '<!doctype html><meta charset=utf-8><title>App Store screenshots</title>'
	echo '<style>body{font:14px -apple-system,sans-serif;margin:24px;background:#f4f4f6}h2{margin:24px 0 8px}div{display:flex;gap:12px;flex-wrap:wrap}figure{margin:0}img{height:420px;border:1px solid #ccc;border-radius:8px;background:#fff}figcaption{color:#666}</style>'
	for d in "$OUT"/*/; do
		[ -d "$d" ] || continue
		s=$(basename "$d")
		echo "<h2>$s · $(size_of "$s")</h2><div>"
		for f in "$d"*.png; do
			[ -e "$f" ] || continue
			echo "<figure><img src=\"$s/$(basename "$f")\"><figcaption>$(basename "$f")</figcaption></figure>"
		done
		echo '</div>'
	done
} >"$OUT/index.html"
echo "store-shots: $OUT/index.html"
