# frozen_string_literal: true

# Written by scripts/release/cask.sh for each release; don't edit by hand.
cask "terminatr-app" do
  version "0.69.0"
  sha256 "75646b2d1fb8b9dd0cf8bed5fe86e7e4465d87bf7c9af6dc96a85e6da433cb68"

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
