# frozen_string_literal: true

# Written by scripts/release/cask.sh for each release; don't edit by hand.
cask "terminatr-app" do
  version "0.73.0"
  sha256 "1f5c8cab632182d8d993808b259ca88c688225e6f384ef20f5aaa2f262d2302a"

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
