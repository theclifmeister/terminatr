# frozen_string_literal: true

# Written by scripts/release/cask.sh for each release; don't edit by hand.
cask "terminatr-app" do
  version "0.74.0"
  sha256 "e708f78d21f20f1d3ec07407548b6ec83309042db512d51c3daf1db10556613b"

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
    `tm update` updates both.
  EOS
end
