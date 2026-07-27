# Homebrew formula for ike.
#
# Built from source rather than shipped as a cask. Homebrew is disabling every
# cask that fails a macOS Gatekeeper check on 2026-09-01 and removing the
# --no-quarantine escape hatch, so distributing unsigned prebuilt macOS
# binaries as a cask is a dead end without an Apple Developer account and
# notarization. A source build compiles locally, so no quarantine attribute is
# ever applied, and it works on Linuxbrew too.
#
# The `url` and `sha256` below are rewritten at release time by
# .github/workflows/release.yml, which copies this file into the tap. Keeping
# the canonical copy here means it is reviewed alongside the code it builds.
class Ike < Formula
  desc "Eisenhower matrix task manager for your terminal"
  homepage "https://github.com/jonascript/ike"
  url "https://github.com/jonascript/ike/archive/refs/tags/v0.0.0.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "MIT"
  head "https://github.com/jonascript/ike.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X github.com/jonascript/ike/internal/cli.version=#{version}"
    system "go", "build", *std_go_args(ldflags:)
  end

  test do
    # The binary reports the version it was stamped with, not "dev".
    assert_match version.to_s, shell_output("#{bin}/ike --version")

    # Exercise the three-frontend store against a scratch data file, never the
    # real matrix. IKE_DATA_FILE must be absolute with an existing parent.
    ENV["IKE_DATA_FILE"] = testpath/"tasks.json"

    system bin/"ike", "add", "write a formula", "-q", "1"
    assert_match "write a formula", shell_output("#{bin}/ike list")

    # The data file must not be readable by other users on the machine.
    assert_equal "100600", (testpath/"tasks.json").stat.mode.to_s(8)

    # Agent access is off by default, so the MCP server must refuse to start.
    output = shell_output("#{bin}/ike mcp 2>&1", 1)
    assert_match "MCP access is off", output
  end
end
