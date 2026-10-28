#!/usr/bin/env bash
# Prints the CI cache keys derived from the toolchain pins in the Makefile,
# as `zig=<key>`, `ghostty=<key>` and `zigpkgs=<key>` lines, and the goreleaser pin as
# `goreleaser=<version>` (append them to $GITHUB_OUTPUT).
# Used by .github/actions/setup and .github/actions/release-setup, so the
# keys are defined in one place.
#
# zig: the Zig version and checksums. ghostty: the Ghostty pin and the Zig
# that builds it, plus the checkout path, because the build's pkg-config
# files name their absolute prefix. zigpkgs: Ghostty's Zig dependencies
# (Zig's global package cache), which only the Ghostty pin and the Zig
# that fetches them decide.
set -euo pipefail

echo "zig=$(grep -E '^ZIG_(VERSION|SHA256_)' Makefile | git hash-object --stdin)"
echo "ghostty=$({ grep -E '^(GHOSTTY_(REV|REPO|CPU)|ZIG_VERSION) ' Makefile; echo "${GITHUB_WORKSPACE:-$PWD}"; } | git hash-object --stdin)"
echo "zigpkgs=$(grep -E '^(GHOSTTY_(REV|REPO)|ZIG_VERSION) ' Makefile | git hash-object --stdin)"

echo "goreleaser=$(sed -n 's/^GORELEASER_VERSION ?= *//p' Makefile)"
