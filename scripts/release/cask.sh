#!/bin/sh
# Writes the Homebrew cask of the Mac app for a release to stdout, from
# the release's checksums.txt. The release workflow runs it with
# formula.sh and opens one pull request with both (Casks/terminatr-app.rb
# beside Formula/terminatr.rb, in the tap this repository is):
#
#   scripts/release/cask.sh 0.54.0 dist/checksums.txt > Casks/terminatr-app.rb
#
# The cask installs terminatr-app.zip (scripts/release/app.sh): signed
# with tm's Developer ID, notarised and stapled, so it passes the
# Gatekeeper check Homebrew requires of casks. It doesn't depend on the
# formula: the app finds whichever tm is installed (Homebrew's or the
# install script's), and one tm per machine is the rule.
set -eu
version=${1:?usage: cask.sh <version> <checksums.txt>}
sums=${2:?usage: cask.sh <version> <checksums.txt>}
version=${version#v}
sha=$(awk '$2 == "terminatr-app.zip" {print $1}' "$sums")
[ -n "$sha" ] || { echo "cask.sh: no terminatr-app.zip in $sums" >&2; exit 1; }
cat <<RUBY
# frozen_string_literal: true

# Written by scripts/release/cask.sh for each release; don't edit by hand.
cask "terminatr-app" do
  version "$version"
  sha256 "$sha"

  url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/terminatr-app.zip"
  name "terminatr"
  desc "Desktop app for terminatr: projects, threads and their conversations"
  homepage "https://github.com/theclifmeister/terminatr"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: :sonoma

  app "Terminatr.app"

  uninstall quit: "dev.terminatr.app"

  zap trash: [
    "~/Library/Preferences/dev.terminatr.app.plist",
    "~/Library/Saved Application State/dev.terminatr.app.savedState",
  ]

  caveats <<~EOS
    The app runs the installed tm (brew install theclifmeister/terminatr/terminatr,
    or the install script), which it needs at the same release or newer.
    \`tm update\` updates both.
  EOS
end
RUBY
