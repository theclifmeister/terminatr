#!/bin/sh
# Checks the release archives in a goreleaser dist/ folder: checksums,
# contents, what each binary links, the glibc floor of the Linux
# binaries, and `tm selftest` for the host's own archive.
#
#   scripts/release/check.sh [--signed] [dist]
#
# --signed (the release workflow, on macOS) also requires each darwin
# binary to carry a Developer ID signature with the hardened runtime and
# to be notarised: Gatekeeper's own assessment must say "Notarized
# Developer ID". `spctl --type execute` rejects every bare binary ("does
# not seem to be an app"), so the check asks for the primary signature
# as `--type open` does for a downloaded file.
#
# The Mac app (scripts/release/app.sh) is checked too: terminatr-app.zip
# and .dmg each hold a universal Terminatr.app of the release's version,
# with the icon its Info.plist names;
# with --signed, signed by tm's team, notarised and stapled.
set -eu
signed=
if [ "${1:-}" = --signed ]; then
	signed=1
	shift
fi
dist=${1:-dist}
glibc_floor=2.28
fail() { echo "check: $*" >&2; exit 1; }

(cd "$dist" && shasum -a 256 -c checksums.txt) || fail "checksums do not match"

host_os=$(uname -s | tr A-Z a-z)
host_arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
n=0
for a in "$dist"/tm_*.tar.gz "$dist"/tm_*.zip; do
	[ -e "$a" ] || continue
	n=$((n + 1))
	name=$(basename "$a")
	name=${name%.tar.gz}
	name=${name%.zip}
	os=$(echo "$name" | awk -F_ '{print $(NF-1)}')
	arch=$(echo "$name" | awk -F_ '{print $NF}')
	d="$tmp/$name"
	mkdir -p "$d"
	bin=tm
	case $a in
	*.zip) unzip -q "$a" -d "$d"; bin=tm.exe ;;
	*) tar -xzf "$a" -C "$d" ;;
	esac
	for f in $bin LICENSE README.md; do
		[ -f "$d/$f" ] || fail "$name lacks $f"
	done
	desc=$(file -b "$d/$bin")
	case "$os/$arch" in
	darwin/arm64) want="Mach-O 64-bit executable arm64" ;;
	darwin/amd64) want="Mach-O 64-bit executable x86_64" ;;
	linux/arm64) want="ELF 64-bit LSB executable, ARM aarch64" ;;
	linux/amd64) want="ELF 64-bit LSB executable, x86-64" ;;
	windows/arm64) want="PE32+ executable (console) Aarch64" ;;
	windows/amd64) want="PE32+ executable (console) x86-64" ;;
	*) fail "$name: unexpected target $os/$arch" ;;
	esac
	case $desc in "$want"*) ;; *) fail "$name: tm is '$desc', want '$want'" ;; esac
	if [ "$os" = windows ]; then
		# ConPTY ships next to tm.exe (scripts/release/conpty.sh).
		for f in conpty.dll OpenConsole.exe; do
			[ -f "$d/$f" ] || fail "$name lacks $f"
		done
	fi
	if [ "$os" = darwin ] && command -v otool >/dev/null; then
		# Only libraries every Mac has: /usr/lib and system frameworks.
		# Which ones depends on the Go release (Go 1.26 on arm64 adds
		# CoreFoundation and libobjc); anything else (Homebrew, a
		# dynamic libghostty) would break on a clean machine.
		libs=$(otool -L "$d/tm" | tail -n +2 | awk '{print $1}' | grep -v -e '^/usr/lib/' -e '^/System/Library/Frameworks/' || true)
		[ -z "$libs" ] || fail "$name links non-system libraries: $libs"
		# Linked by Apple's ld (scripts/release/cc.sh): chained fixups
		# keep __DATA_CONST clean, where zig's old-style rebase info left
		# ~2.4 MB dirty in every tm process; and still runs on macOS 13.
		loads=$(otool -l "$d/tm")
		echo "$loads" | grep -q 'cmd LC_DYLD_CHAINED_FIXUPS' || fail "$name: no chained fixups (LC_DYLD_CHAINED_FIXUPS)"
		minos=$(echo "$loads" | awk '$1 == "minos" {print $2; exit}')
		[ -n "$minos" ] || fail "$name: no minos found in otool -l output"
		[ "$minos" = 13.0 ] || fail "$name: minimum macOS is $minos, want 13.0"
	fi
	if [ "$os" = darwin ] && [ -n "$signed" ]; then
		v=$(codesign --verify --strict "$d/tm" 2>&1) || fail "$name: signature: $v"
		info=$(codesign -d --verbose=2 "$d/tm" 2>&1)
		echo "$info" | grep -q '^Authority=Developer ID Application' || fail "$name: not signed with a Developer ID"
		echo "$info" | grep -q 'flags=.*runtime' || fail "$name: no hardened runtime"
		gk=$(spctl --assess --type open --context context:primary-signature -v "$d/tm" 2>&1) ||
			fail "$name: Gatekeeper rejects it: $gk"
		echo "$gk" | grep -q 'source=Notarized Developer ID' || fail "$name: not notarised: $gk"
		echo "$name: $(echo "$info" | grep '^TeamIdentifier='), notarised"
		team=$(echo "$info" | sed -n 's/^TeamIdentifier=//p')
	fi
	if [ "$os" = linux ] && command -v objdump >/dev/null; then
		max=$(objdump -T "$d/tm" | grep -o 'GLIBC_[0-9.]*' | sed 's/GLIBC_//' | sort -t. -k1,1n -k2,2n | tail -1)
		[ -n "$max" ] || fail "$name: no GLIBC_ versions found in objdump -T output"
		top=$(printf '%s\n%s\n' "$max" "$glibc_floor" | sort -t. -k1,1n -k2,2n | tail -1)
		[ "$top" = "$glibc_floor" ] || fail "$name needs glibc $max, above the floor $glibc_floor"
		echo "$name: glibc >= $max"
	fi
	if [ "$os/$arch" = "$host_os/$host_arch" ]; then
		"$d/tm" selftest || fail "$name: tm selftest failed"
		"$d/tm" version
		tm_version=$("$d/tm" version | awk '{print $2}')
	fi
	echo "$name: ok ($desc)" | cut -c1-120
done
[ "$n" -eq 6 ] || fail "$n archives, want 6"

# The relay's Docker files (scripts/release/relay-docker.sh), each compose
# file pinned to the one release.
docker=tm-relay-docker.tar.gz
grep -q " $docker\$" "$dist/checksums.txt" || fail "checksums.txt does not list $docker"
mkdir "$tmp/docker"
tar -xzf "$dist/$docker" -C "$tmp/docker" || fail "$docker: not a tar.gz"
for f in Dockerfile Caddyfile README.md compose.yaml compose.acme.yaml compose.selfsigned.yaml; do
	[ -f "$tmp/docker/tm-relay-docker/$f" ] || fail "$docker lacks tm-relay-docker/$f"
done
pins=$(cat "$tmp"/docker/tm-relay-docker/compose*.yaml | grep -o '\${TM_VERSION:-[^}]*}' | sort -u)
case $pins in
'${TM_VERSION:-v'[0-9]*'}') echo "$docker: ok (pinned to $(echo "$pins" | sed 's/.*:-//; s/}//'))" ;;
*) fail "$docker: compose files pin TM_VERSION to '$(echo $pins)', want one release tag" ;;
esac

# The Mac app. Its CFBundleShortVersionString is the release's three
# numbers (a snapshot's v0.54.1-snapshot-abc is 0.54.1), in an XML
# Info.plist, which tm update reads (internal/update, app.go).
app_version=${tm_version:-}
app_version=${app_version#v}
app_version=${app_version%%-*}
check_app() { # <Terminatr.app> <where>
	a=$1
	where=$2
	plist="$a/Contents/Info.plist"
	[ -f "$plist" ] || fail "$where: no Terminatr.app/Contents/Info.plist"
	head -c 5 "$plist" | grep -q '<?xml' || fail "$where: Info.plist is not XML"
	id=$(plutil -extract CFBundleIdentifier raw -o - "$plist")
	[ "$id" = dev.terminatr.app ] || fail "$where: CFBundleIdentifier is $id, want dev.terminatr.app"
	min=$(plutil -extract LSMinimumSystemVersion raw -o - "$plist")
	[ "$min" = 14.0 ] || fail "$where: LSMinimumSystemVersion is $min, want 14.0"
	v=$(plutil -extract CFBundleShortVersionString raw -o - "$plist")
	[ -z "$app_version" ] || [ "$v" = "$app_version" ] || fail "$where: version $v, want $app_version (tm's)"
	archs=$(lipo -archs "$a/Contents/MacOS/Terminatr")
	for arch in arm64 x86_64; do
		case " $archs " in *" $arch "*) ;; *) fail "$where: Terminatr is $archs, want arm64 and x86_64" ;; esac
	done
	# The icon (macos/Icon): the .icns for macOS 14 and 15, Assets.car's for 26+.
	icns=$(plutil -extract CFBundleIconFile raw -o - "$plist")
	[ -f "$a/Contents/Resources/${icns%.icns}.icns" ] || fail "$where: no Resources/${icns%.icns}.icns (CFBundleIconFile)"
	icon=$(plutil -extract CFBundleIconName raw -o - "$plist")
	xcrun assetutil --info "$a/Contents/Resources/Assets.car" | grep -q "\"Name\" : \"$icon\"" ||
		fail "$where: Assets.car has no icon $icon (CFBundleIconName)"
	if [ -z "$signed" ]; then
		echo "$where: ok (Terminatr.app $v, $archs, unsigned)"
		return
	fi
	out=$(codesign --verify --deep --strict "$a" 2>&1) || fail "$where: signature: $out"
	info=$(codesign -d --verbose=2 "$a" 2>&1)
	echo "$info" | grep -q '^Authority=Developer ID Application' || fail "$where: not signed with a Developer ID"
	echo "$info" | grep -q 'flags=.*runtime' || fail "$where: no hardened runtime"
	t=$(echo "$info" | sed -n 's/^TeamIdentifier=//p')
	[ -n "$t" ] && [ "$t" = "${team:-}" ] || fail "$where: signed by team '$t', tm by '${team:-}'"
	gk=$(spctl --assess --type execute -v "$a" 2>&1) || fail "$where: Gatekeeper rejects it: $gk"
	echo "$gk" | grep -q 'source=Notarized Developer ID' || fail "$where: not notarised: $gk"
	xcrun stapler validate "$a" >/dev/null || fail "$where: no stapled ticket"
	echo "$where: ok (Terminatr.app $v, team $t, $archs, notarised and stapled)"
}
for f in terminatr-app.zip terminatr-app.dmg; do
	grep -q " $f\$" "$dist/checksums.txt" || fail "checksums.txt does not list $f"
done
mkdir "$tmp/app"
ditto -x -k "$dist/terminatr-app.zip" "$tmp/app" || fail "terminatr-app.zip: not a zip"
[ "$(ls "$tmp/app")" = Terminatr.app ] || fail "terminatr-app.zip holds $(ls "$tmp/app" | tr '\n' ' '), want Terminatr.app alone"
check_app "$tmp/app/Terminatr.app" terminatr-app.zip
dmg="$dist/terminatr-app.dmg"
if [ -n "$signed" ]; then
	gk=$(spctl --assess --type open --context context:primary-signature -v "$dmg" 2>&1) ||
		fail "terminatr-app.dmg: Gatekeeper rejects it: $gk"
	echo "$gk" | grep -q 'source=Notarized Developer ID' || fail "terminatr-app.dmg: not notarised: $gk"
	xcrun stapler validate "$dmg" >/dev/null || fail "terminatr-app.dmg: no stapled ticket"
fi
mkdir "$tmp/dmg"
hdiutil attach -quiet -nobrowse -readonly -mountpoint "$tmp/dmg" "$dmg" || fail "terminatr-app.dmg: does not mount"
rc=0
(check_app "$tmp/dmg/Terminatr.app" terminatr-app.dmg) || rc=$?
[ -L "$tmp/dmg/Applications" ] || rc=1
# The volume's icon: .VolumeIcon.icns and the root's custom-icon flag (C).
[ -f "$tmp/dmg/.VolumeIcon.icns" ] && GetFileInfo -a "$tmp/dmg" | grep -q C || rc=1
hdiutil detach -quiet "$tmp/dmg" || true
[ "$rc" -eq 0 ] || fail "terminatr-app.dmg: the app, its Applications link or the volume's icon is wrong"
