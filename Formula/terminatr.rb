# frozen_string_literal: true

# Written by scripts/release/formula.sh for each release; don't edit by hand.
class Terminatr < Formula
  desc "Terminal workspace where coding agents work through a project's tasks"
  homepage "https://github.com/theclifmeister/terminatr"
  version "0.41.0"
  license "MIT"

  livecheck do
    url :stable
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_darwin_arm64.tar.gz"
      sha256 "861664a3a9fe525ad87667508e01e6829ecfebbc650bfdf043b6f5c5f15f552f"
    end
    on_intel do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_darwin_amd64.tar.gz"
      sha256 "41c566348aa702fdb42c4a75ae275f043f05cb1a4931f4c59342d081c2e52779"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_linux_arm64.tar.gz"
      sha256 "e5bc1c5e63ebe5200a625b2d7debf6c2cecfa361800600daad01b56b15ad310b"
    end
    on_intel do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_linux_amd64.tar.gz"
      sha256 "17b78f1b7c4ff7f26ce43f12c61a2285af470a5b4d4127fde4054ace5a5cfbaa"
    end
  end

  def install
    bin.install "tm"
    doc.install "README.md", "docs/OPERATIONS.md"
  end

  def caveats
    <<~EOS
      A running tm server keeps the old binary after `brew upgrade`;
      `tm server restart` switches to the new one (agents are resumed).
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/tm version")
    system bin/"tm", "selftest"
  end
end
