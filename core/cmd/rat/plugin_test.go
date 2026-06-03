package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPluginInitCheck covers the authoring loop (ADR-026): `rat plugin init` scaffolds a
// folder that PASSES `rat plugin check`, a known kind is required, and a broken manifest fails.
func TestPluginInitCheck(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var out bytes.Buffer

	// init a known-kind plugin → a buildable folder.
	if err := runPluginInit([]string{"demo-state", "--kind", "state-backend"}, &out); err != nil {
		t.Fatalf("init: %v", err)
	}
	for _, f := range []string{"manifest.yaml", "server.py", "Dockerfile", "README.md", "ci.sh", ".github/workflows/plugin.yml"} {
		if _, err := os.Stat(filepath.Join("demo-state", f)); err != nil {
			t.Errorf("scaffold missing %s: %v", f, err)
		}
	}

	// the scaffolded folder passes check.
	out.Reset()
	if err := runPluginCheck([]string{"demo-state"}, &out); err != nil {
		t.Fatalf("check (scaffolded) should pass: %v", err)
	}
	if !strings.Contains(out.String(), "state-backend") || !strings.Contains(out.String(), "3 provides") {
		t.Errorf("check output unexpected: %q", out.String())
	}

	// init refuses an unknown kind.
	if err := runPluginInit([]string{"x", "--kind", "not-an-axis"}, &out); err == nil {
		t.Error("init should reject an unknown kind")
	}

	// a broken manifest fails check (bad kind).
	mf := filepath.Join("demo-state", "manifest.yaml")
	b, _ := os.ReadFile(mf)
	_ = os.WriteFile(mf, []byte(strings.Replace(string(b), "kind: state-backend", "kind: bogus-kind", 1)), 0o644)
	if err := runPluginCheck([]string{"demo-state"}, &out); err == nil {
		t.Error("check should fail on an unknown kind")
	}
}
