class Gum < Formula
  desc "Google Universal MCP CLI and stdio server"
  homepage "https://github.com/ehmo/gum"
  url "https://github.com/ehmo/gum.git", tag: "v1.0.0"
  license "FSL-1.1-ALv2"
  head "https://github.com/ehmo/gum.git", branch: "main"

  depends_on "go" => :build

  def install
    cd "apps/gum" do
      system "go", "build", "-trimpath", "-ldflags", "-s -w -X main.version=#{version}", "-o", bin/"gum", "./cmd/gum"
    end
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gum --version")
    assert_match "core", shell_output("#{bin}/gum skills list --format json")
    assert_match "actions", shell_output("#{bin}/gum agents install --target all --features skills,mcp --dry-run --format json")
    assert_match "auth", shell_output("#{bin}/gum doctor --format=json", 1)
  end
end
