#!/bin/sh
# Packs deploy/relay's Docker files into
# .build/release/tm-relay-docker.tar.gz, a release asset next to the
# archives, listed in checksums.txt (.goreleaser.yaml runs this as a before
# hook; make copies the file into dist/ afterwards, for check.sh): the
# Dockerfile, the compose files, the Caddyfile and the README, in a
# tm-relay-docker/ folder, with the compose files' TM_VERSION pinned to
# this release.
#
#   scripts/release/relay-docker.sh v0.28.0
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
version=${1:?usage: relay-docker.sh <version tag>}
case $version in v[0-9]*) ;; *) echo "relay-docker: '$version' is not a version tag" >&2; exit 1 ;; esac
src="$root/deploy/relay"
out="$root/.build/release/tm-relay-docker.tar.gz"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/relay-docker.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
d="$tmp/tm-relay-docker"
mkdir -p "$d"
cp "$src/Dockerfile" "$src/Caddyfile" "$src/README.md" "$d/"
for f in compose.yaml compose.acme.yaml compose.selfsigned.yaml; do
	grep -q '\${TM_VERSION:-latest}' "$src/$f" || { echo "relay-docker: $f has no \${TM_VERSION:-latest} to pin" >&2; exit 1; }
	sed "s/\${TM_VERSION:-latest}/\${TM_VERSION:-$version}/g" "$src/$f" > "$d/$f"
done
chmod 755 "$d"
chmod 644 "$d"/*
mkdir -p "$root/.build/release"
# No macOS extended attributes (._ files) in the archive.
(cd "$tmp" && COPYFILE_DISABLE=1 tar -czf "$out" tm-relay-docker)
echo "relay-docker: $out ($version)"
