package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WellWells/agentswap/internal/transfer"
)

func TestCLIExportImport(t *testing.T) {
	h := newHarness(t)

	// Setup accounts across all 3 providers
	// 1. Codex
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add", "cx-alice")

	// 2. Claude
	h.claudeLogin("uuid-c1", "org-c1", "claude@x.com", "tok-c1")
	h.run("ccswap", "add", "cc-work")

	// 3. Antigravity
	h.agyLogin("s1", "agy@x.com", "g1")
	h.run("agswap", "add", "ag-main")

	// Test 1: agentswap export to stdout
	code, out, errs := h.run("agentswap", "export")
	if code != 0 {
		t.Fatalf("agentswap export failed: %d, err: %s", code, errs)
	}

	var b transfer.Bundle
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("export output is not valid json bundle: %v\nOutput:\n%s", err, out)
	}
	if len(b["codex"]) != 1 || b["codex"][0].Alias != "cx-alice" {
		t.Fatalf("unexpected codex in bundle: %+v", b["codex"])
	}
	if len(b["claude"]) != 1 || b["claude"][0].Alias != "cc-work" {
		t.Fatalf("unexpected claude in bundle: %+v", b["claude"])
	}
	if len(b["antigravity"]) != 1 || b["antigravity"][0].Alias != "ag-main" {
		t.Fatalf("unexpected agy in bundle: %+v", b["antigravity"])
	}

	// Test 2: agentswap export to file
	outFile := filepath.Join(h.home, "bundle.json")
	code, out, errs = h.run("agentswap", "export", outFile)
	if code != 0 {
		t.Fatalf("export to file failed: %d, %s", code, errs)
	}
	if !strings.Contains(out, "Exported to") {
		t.Fatalf("expected export message, got %s", out)
	}
	fileBytes, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}

	// Test 3: agentswap import without args should fail with usage error (code 2)
	code, _, _ = h.run("agentswap", "import")
	if code != 2 {
		t.Fatalf("agentswap import without args should exit with 2, got %d", code)
	}

	// Test 4: import in a fresh harness
	h2 := newHarness(t)
	code, out, errs = h2.run("agentswap", "import", outFile)
	if code != 0 {
		t.Fatalf("import to h2 failed: %d, %s", code, errs)
	}
	if !strings.Contains(out, "alice@x.com") || !strings.Contains(out, "claude@x.com") || !strings.Contains(out, "agy@x.com") {
		t.Fatalf("import output missing imported accounts: %s", out)
	}

	// Check status in h2
	code, out, _ = h2.run("cxswap", "list")
	if code != 0 || !strings.Contains(out, "cx-alice") {
		t.Fatalf("h2 cxswap list: %s", out)
	}
	code, out, _ = h2.run("ccswap", "list")
	if code != 0 || !strings.Contains(out, "cc-work") {
		t.Fatalf("h2 ccswap list: %s", out)
	}
	code, out, _ = h2.run("agswap", "list")
	if code != 0 || !strings.Contains(out, "ag-main") {
		t.Fatalf("h2 agswap list: %s", out)
	}

	// Test 5: import from stdin (-)
	h3 := newHarness(t)
	h3.stdin = string(fileBytes)
	code, out, errs = h3.run("agentswap", "import", "-")
	if code != 0 {
		t.Fatalf("import from stdin failed: %d, %s", code, errs)
	}
	code, out, _ = h3.run("cxswap", "list")
	if code != 0 || !strings.Contains(out, "cx-alice") {
		t.Fatalf("h3 cxswap list: %s", out)
	}

	// Test 6: Single provider export and import
	cxFile := filepath.Join(h.home, "cx.json")
	code, _, _ = h.run("cxswap", "export", cxFile)
	if code != 0 {
		t.Fatalf("cxswap export failed")
	}
	var cxBundle transfer.Bundle
	cxData, _ := os.ReadFile(cxFile)
	json.Unmarshal(cxData, &cxBundle)
	if len(cxBundle) != 1 || len(cxBundle["codex"]) != 1 {
		t.Fatalf("cxBundle should only have codex: %+v", cxBundle)
	}

	// Overwrite import via cxswap
	cxBundle["codex"][0].Alias = "cx-renamed"
	cxRenamed, _ := json.Marshal(cxBundle)
	os.WriteFile(cxFile, cxRenamed, 0o600)
	code, out, _ = h.run("cxswap", "import", cxFile)
	if code != 0 {
		t.Fatalf("cxswap import failed: %d", code)
	}
	code, out, _ = h.run("cxswap", "list")
	if !strings.Contains(out, "cx-renamed") {
		t.Fatalf("cxswap list should have cx-renamed: %s", out)
	}

	// Test 7: agswap export and import
	agFile := filepath.Join(h.home, "ag.json")
	code, _, _ = h.run("agswap", "export", agFile)
	if code != 0 {
		t.Fatalf("agswap export failed")
	}
	code, _, _ = h.run("agswap", "import", agFile)
	if code != 0 {
		t.Fatalf("agswap import failed")
	}

	// Test 8: ccswap export and import
	ccFile := filepath.Join(h.home, "cc.json")
	code, _, _ = h.run("ccswap", "export", ccFile)
	if code != 0 {
		t.Fatalf("ccswap export failed")
	}
	code, _, _ = h.run("ccswap", "import", ccFile)
	if code != 0 {
		t.Fatalf("ccswap import failed")
	}

	// Test 9: Verify agentswap help contains export and import
	code, out, _ = h.run("agentswap", "help")
	if code != 0 || !strings.Contains(out, "agentswap export") || !strings.Contains(out, "agentswap import") {
		t.Fatalf("agentswap help missing export/import:\n%s", out)
	}
}
