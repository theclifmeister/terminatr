#!/bin/sh
# Builds the Mac app (macos/, docs/SPEC.md §3.8) for a release and packs
# it into .build/release/app/: terminatr-app.zip, which the cask and
# `tm update` install, and terminatr-app.dmg, for a download by hand.
# Both are release assets listed in checksums.txt (.goreleaser.yaml runs
# this as a before hook; make copies them into dist/ afterwards, for
# check.sh):
#
#   scripts/release/app.sh v0.54.0
#
# The app is universal (arm64 and x86_64) and needs macOS 14. With
# TM_SIGN_IDENTITY (the release workflow) it is signed with the same
# Developer ID as tm, hardened runtime and a secure timestamp; with
# TM_NOTARY_KEY too, the app is notarised and stapled before it is
# zipped, and the DMG, signed in turn, is notarised and stapled as well
# (scripts/release/notarize.sh). Stapled, both pass Gatekeeper offline.
# Without TM_SIGN_IDENTITY (a snapshot, a fork) the app is unsigned and
# the script says so.
#
# Building needs Xcode with its Metal toolchain (SwiftTerm compiles a
# shader; macos/Makefile).
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
tag=${1:?usage: app.sh <version tag>}
case $tag in v[0-9]*) ;; *) echo "app: '$tag' is not a version tag" >&2; exit 1 ;; esac
# CFBundleShortVersionString is three numbers: v0.54.0-rc1 and a
# snapshot's v0.54.1-snapshot-abc1234 are 0.54.0 and 0.54.1.
version=${tag#v}
version=${version%%-*}
out="$root/.build/release/app"
app="$root/macos/build/Terminatr.app"

make -C "$root/macos" app ARCHS="arm64 x86_64" VERSION="$version"
rm -rf "$out"
mkdir -p "$out"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/tm-app.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

signed=
notarised=
if [ -z "${TM_SIGN_IDENTITY:-}" ]; then
	echo "app: TM_SIGN_IDENTITY not set; Terminatr.app is not signed"
else
	# No entitlements: the app runs tm as a child, draws with Metal and
	# posts notifications, none of which the hardened runtime restricts.
	# The identifier is Info.plist's (dev.terminatr.app). The resource
	# bundles in Contents/Resources hold no code and are sealed as files.
	msg=$(codesign --force --sign "$TM_SIGN_IDENTITY" --options runtime --timestamp "$app" 2>&1) ||
		{ echo "$msg" >&2; exit 1; }
	codesign --verify --deep --strict --verbose=2 "$app"
	codesign -d --verbose=2 "$app" 2>&1 | grep -q 'flags=.*runtime' || {
		echo "app: $app is not hardened-runtime signed" >&2
		exit 1
	}
	signed=1
	echo "app: Terminatr.app signed with $TM_SIGN_IDENTITY"
	if [ -z "${TM_NOTARY_KEY:-}" ]; then
		echo "app: TM_NOTARY_KEY not set; Terminatr.app is signed but not notarised"
	else
		ditto -c -k --keepParent "$app" "$tmp/notarize.zip"
		"$root/scripts/release/notarize.sh" "$tmp/notarize.zip" Terminatr.app
		xcrun stapler staple "$app"
		xcrun stapler validate "$app"
		notarised=1
	fi
fi

# The zip holds Terminatr.app, ticket included (stapler writes it into
# the bundle, so it must come before the zip).
ditto -c -k --keepParent "$app" "$out/terminatr-app.zip"

# The DMG: the app and a link to /Applications to drag it onto.
mkdir "$tmp/dmg"
ditto "$app" "$tmp/dmg/Terminatr.app"
ln -s /Applications "$tmp/dmg/Applications"
hdiutil create -quiet -volname terminatr -srcfolder "$tmp/dmg" -fs HFS+ -format UDZO -ov "$out/terminatr-app.dmg"
if [ -n "$signed" ]; then
	codesign --force --sign "$TM_SIGN_IDENTITY" --timestamp "$out/terminatr-app.dmg"
	codesign --verify --strict --verbose=2 "$out/terminatr-app.dmg"
fi
if [ -n "$notarised" ]; then
	"$root/scripts/release/notarize.sh" "$out/terminatr-app.dmg" terminatr-app.dmg
	xcrun stapler staple "$out/terminatr-app.dmg"
	xcrun stapler validate "$out/terminatr-app.dmg"
fi
echo "app: $(cd "$out" && ls | tr "\n" " ")(version $version)"
