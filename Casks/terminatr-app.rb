# frozen_string_literal: true

# Written by scripts/release/cask.sh for each release; don't edit by hand.
cask "terminatr-app" do
  version "0.75.0"
  sha256 "dbbbe7fd3c0ec1d9be865644e40ce2374bc18cd23a077140eef1a666b9ba9768"

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
