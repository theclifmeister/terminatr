# frozen_string_literal: true

# Written by scripts/release/formula.sh for each release; don't edit by hand.
class Terminatr < Formula
  desc "Terminal workspace where coding agents work through a project's tasks"
  homepage "https://github.com/theclifmeister/terminatr"
  version "0.27.2"
  license "MIT"

  livecheck do
    url :stable
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_darwin_arm64.tar.gz"
      sha256 "315589cee430a0ea19da5c0b510ac0d3906bd030ab32f0dd31f47fd2d44bcd6a"
    end
    on_intel do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_darwin_amd64.tar.gz"
      sha256 "b00126aae2744983c731c0893e7987c7fbadafbdb01d8a83d353f7ef410feb74"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_linux_arm64.tar.gz"
      sha256 "d19941a89ca79b72a53d6e23f1b9486a1bb448110d9e41097123120e8c3dab80"
    end
    on_intel do
      url "https://github.com/theclifmeister/terminatr/releases/download/v#{version}/tm_linux_amd64.tar.gz"
      sha256 "f37a450312bf033c8c201c109007b71e84cf6e6e71e6b64e15e7a94053372d11"
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
