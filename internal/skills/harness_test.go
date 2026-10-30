package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The harness lists hooks, MCP servers, plugins, settings and memory, and
// never shows secret values.
func TestScanHarness(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".claude", "settings.json"), `{"model":"opus","permissions":{"allow":["Bash(ls)"]},
		"hooks":{"Stop":[{"hooks":[{"type":"command","command":"notify"}]}]},
		"enabledPlugins":{"a@m":true,"b@m":false},"env":{"API_TOKEN":"s3cret"}}`)
	write(filepath.Join(home, ".claude.json"), `{"mcpServers":{"fig":{"command":"npx","args":["fig","--api-key","s3cret"]},
		"web":{"url":"https://x.dev/mcp?token=s3cret&v=1"}}}`)
	write(filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"db":{"command":"db-mcp","env":{"PASSWORD":"s3cret"}}}}`)
	write(filepath.Join(proj, "CLAUDE.md"), "# Rules\nbe nice\n")
	items := ScanHarness(home, []string{proj})
	kinds := map[Kind]int{}
	var all strings.Builder
	for _, it := range items {
		kinds[it.Kind]++
		all.WriteString(it.Name + " " + it.Description + " " + strings.Join(it.Detail, " ") + "\n")
	}
	want := map[Kind]int{Settings: 1, Hook: 1, Plugin: 2, MCP: 3, Memory: 1}
	for k, n := range want {
		if kinds[k] != n {
			t.Errorf("%s: %d, want %d", k, kinds[k], n)
		}
	}
	if strings.Contains(all.String(), "s3cret") {
		t.Errorf("a secret value is shown:\n%s", all.String())
	}
	if !strings.Contains(all.String(), "API_TOKEN") || !strings.Contains(all.String(), "PASSWORD") {
		t.Error("env names should be listed")
	}
}
