# frozen_string_literal: true

# Written by scripts/release/formula.sh for each release; don't edit by hand.
class Terminatr < Formula
  desc "Terminal workspace where coding agents work through a project's tasks"
  homepage "https://github.com/theclifmeister/terminatr"
  version "0.28.2"
  license "MIT"

  livecheck do
    url :stable
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_darwin_arm64.tar.gz"
      sha256 "6e07d4f5d2ef8c80448e7cf3f735b32ca54cf9ec20dba795a52059377caee54c"
    end
    on_intel do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_darwin_amd64.tar.gz"
      sha256 "53e738f2d458a03ac3f26866bb8253936b5dcaebbef891c925e27e1347ba7e33"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_linux_arm64.tar.gz"
      sha256 "e0713f4f5e778b136bc4585e691c56143f0d9aa95dbb1556b1bb69447f8f8140"
    end
    on_intel do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_linux_amd64.tar.gz"
      sha256 "990c16b59cf01dce89886c57e9ebfbc4c5a63b73580bd814e1ffb6a90232dd42"
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
