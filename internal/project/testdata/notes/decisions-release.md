+++
title   = "Releases: release-please, the Homebrew formula, checksums"
touches = [".github/workflows/release.yml", "repo:homebrew-tap"]
source  = "T120"
+++
## Decided
- release-please opens the release PR from Conventional Commits; the user merges it.
- After each release the formula's sha256s are checked against checksums.txt.

## Ruled out
- Tagging releases by hand.

## Watch out
- The formula bump runs from the tap repo with its own token; a missing token fails silently.
