#!/bin/sh
# Builds the iPhone app (ios/, T797) for a release and uploads it to
# TestFlight. The release workflow's testflight job runs it on the tag:
#
#   scripts/release/ios.sh v0.57.0
#
# The version is the tag's three numbers, as the Mac app's (app.sh); the
# build number is the commit count of the tag (git rev-list --count),
# which only grows along main, so every release, prereleases included,
# uploads a higher build than the last. TM_IOS_BUILD overrides it.
#
# Signing is manual (ios/README.md, TestFlight): TM_IOS_PROFILE, the path
# to the App Store provisioning profile of dev.terminatr.ios, and
# TM_TEAM_ID its team; the matching Apple Distribution identity must be
# in the keychain (the workflow imports it). With TM_ASC_KEY (the path to
# an App Store Connect API key, .p8), TM_ASC_KEY_ID and TM_ASC_ISSUER,
# xcodebuild uploads the export to App Store Connect; without them it
# leaves Terminatr.ipa in .build/release/ios/. Without TM_IOS_PROFILE the
# archive is unsigned and nothing is exported: a dry run, which needs no
# Apple account. Nothing here prints a secret.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
tag=${1:?usage: ios.sh <version tag>}
case $tag in v[0-9]*) ;; *) echo "ios: '$tag' is not a version tag" >&2; exit 1 ;; esac
version=${tag#v}
version=${version%%-*}
out="$root/.build/release/ios"
archive="$out/Terminatr.xcarchive"
bundle=dev.terminatr.ios

if [ -n "${TM_IOS_BUILD:-}" ]; then
	build=$TM_IOS_BUILD
else
	# A shallow clone counts 1: every upload would repeat that number.
	if [ "$(git -C "$root" rev-parse --is-shallow-repository)" = true ]; then
		echo "ios: shallow clone; fetch the full history (fetch-depth: 0) or set TM_IOS_BUILD" >&2
		exit 1
	fi
	build=$(git -C "$root" rev-list --count HEAD)
fi

rm -rf "$out"
mkdir -p "$out"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/tm-ios.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

if [ -z "${TM_IOS_PROFILE:-}" ]; then
	make -C "$root/ios" archive ARCHIVE="$archive" VERSION="$version" BUILD="$build"
	echo "ios: TM_IOS_PROFILE not set; Terminatr.xcarchive is not signed and not uploaded (version $version, build $build)"
	exit 0
fi
team=${TM_TEAM_ID:?ios: TM_IOS_PROFILE needs TM_TEAM_ID}

# The profile must be an App Store one for this app and team, and still
# valid; installed where Xcode looks (both its old and its 16+ folder),
# by its UUID.
security cms -D -i "$TM_IOS_PROFILE" > "$tmp/profile.plist" 2>/dev/null ||
	{ echo "ios: TM_IOS_PROFILE is not a provisioning profile" >&2; exit 1; }
field() { plutil -extract "$1" raw -o - "$tmp/profile.plist" 2>/dev/null || true; }
uuid=$(field UUID)
[ "$(field Entitlements.application-identifier)" = "$team.$bundle" ] ||
	{ echo "ios: the profile is not for $bundle of team TM_TEAM_ID" >&2; exit 1; }
[ "$(field Entitlements.get-task-allow)" = false ] && [ -z "$(field ProvisionedDevices)" ] ||
	{ echo "ios: the profile is not an App Store profile (Distribution → App Store Connect)" >&2; exit 1; }
[ "$(field ExpirationDate)" \> "$(date -u +%Y-%m-%dT%H:%M:%SZ)" ] ||
	{ echo "ios: the profile has expired; make a new one (ios/README.md, TestFlight)" >&2; exit 1; }
for dir in "$HOME/Library/MobileDevice/Provisioning Profiles" "$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles"; do
	mkdir -p "$dir"
	cp "$TM_IOS_PROFILE" "$dir/$uuid.mobileprovision"
done

make -C "$root/ios" archive ARCHIVE="$archive" VERSION="$version" BUILD="$build" PROFILE="$uuid" TEAM="$team"
codesign --verify --deep --strict "$archive/Products/Applications/Terminatr.app"

cp "$root/ios/ExportOptions.plist" "$tmp/export.plist"
plutil -replace teamID -string "$team" "$tmp/export.plist"
plutil -replace provisioningProfiles -json "{\"$bundle\":\"$uuid\"}" "$tmp/export.plist"
if [ -z "${TM_ASC_KEY:-}" ]; then
	plutil -replace destination -string export "$tmp/export.plist"
	xcodebuild -exportArchive -archivePath "$archive" -exportOptionsPlist "$tmp/export.plist" -exportPath "$out"
	echo "ios: TM_ASC_KEY not set; $out/Terminatr.ipa is signed but not uploaded (version $version, build $build)"
	exit 0
fi
: "${TM_ASC_KEY_ID:?ios: TM_ASC_KEY needs TM_ASC_KEY_ID}" "${TM_ASC_ISSUER:?ios: TM_ASC_KEY needs TM_ASC_ISSUER}"
xcodebuild -exportArchive -archivePath "$archive" -exportOptionsPlist "$tmp/export.plist" -exportPath "$out" \
	-authenticationKeyPath "$TM_ASC_KEY" -authenticationKeyID "$TM_ASC_KEY_ID" -authenticationKeyIssuerID "$TM_ASC_ISSUER"
echo "ios: Terminatr $version ($build) uploaded to App Store Connect; TestFlight lists it once processed"
