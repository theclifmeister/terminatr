#!/bin/sh
# Regenerates the app icon from its source, AppIcon.icon (Icon Composer;
# T766, concept B): Bloom's mesh (Assets/mesh-light.svg, mesh-dark.svg,
# Theme.bloom's colours) under the site's ›_ (Assets/glyph.svg,
# site/favicon.svg) in glass. Both outputs are committed; make app
# copies them into Terminatr.app/Contents/Resources:
#
#   Assets.car    actool's compile of AppIcon.icon: the icon macOS 26+
#                 draws, light, dark, tinted and clear (CFBundleIconName)
#   AppIcon.icns  every size, 16 to 512@2x, ictool's default rendition:
#                 what macOS 14 and 15 draw (CFBundleIconFile). actool's
#                 own .icns has four sizes only.
#
# Edit the .icon (in Icon Composer, or icon.json and the SVGs), then:
#
#   macos/Icon/icon.sh
#
# Needs Xcode 26+: actool, and ictool inside Icon Composer.app.
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
dev=$(xcode-select -p)
ictool="$dev/../Applications/Icon Composer.app/Contents/Executables/ictool"
[ -x "$ictool" ] || { echo "icon: no ictool at $ictool (needs Xcode 26+)" >&2; exit 1; }
tmp=$(mktemp -d "${TMPDIR:-/tmp}/tm-icon.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

xcrun actool "$dir/AppIcon.icon" --compile "$tmp" --platform macosx \
	--minimum-deployment-target 14.0 --app-icon AppIcon --include-all-app-icons \
	--output-partial-info-plist "$tmp/partial.plist" >/dev/null
[ -f "$tmp/Assets.car" ] || { echo "icon: actool made no Assets.car" >&2; exit 1; }

mkdir "$tmp/AppIcon.iconset"
for s in 16 32 128 256 512; do
	for x in 1 2; do
		case $x in 1) name=icon_${s}x${s}.png ;; 2) name=icon_${s}x${s}@2x.png ;; esac
		"$ictool" "$dir/AppIcon.icon" --export-image --output-file "$tmp/AppIcon.iconset/$name" \
			--platform macOS --rendition Default --width $s --height $s --scale $x >/dev/null
		[ -f "$tmp/AppIcon.iconset/$name" ] || { echo "icon: ictool made no $name" >&2; exit 1; }
	done
done
iconutil -c icns -o "$tmp/AppIcon.icns" "$tmp/AppIcon.iconset"

cp "$tmp/Assets.car" "$tmp/AppIcon.icns" "$dir/"
echo "icon: wrote $dir/Assets.car and $dir/AppIcon.icns"
