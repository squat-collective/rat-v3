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
	// the kind-aware scaffold derives all of the state axis's capabilities from the linked
	// descriptors (get/put/list/delete/watch), so check reports 5 provides.
	if !strings.Contains(out.String(), "state-backend") || !strings.Contains(out.String(), "5 provides") {
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

// TestPluginCheckDeps covers the dependency coherence `rat plugin check` adds (ADR-026):
// capabilities must name something real, and `provides` must be the plugin's own axis.
func TestPluginCheckDeps(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write := func(name, body string) {
		_ = os.MkdirAll(name, 0o755)
		_ = os.WriteFile(filepath.Join(name, "manifest.yaml"), []byte(body), 0o644)
	}
	hdr := func(kind, name string) string {
		// resources.requests is required by the authoring gate (ADR-039); include it so these
		// dep-coherence fixtures exercise the dep checks, not the resources check.
		return "api_version: rat/1\nkind: " + kind + "\nmetadata:\n  name: " + name + "\n  version: 0.1.0\ncompatible_core: [\"rat/1\"]\nresources:\n  requests:\n    cpu: \"50m\"\n    memory: \"32Mi\"\n"
	}
	var out bytes.Buffer

	// valid: a strategy provides strategy/apply, requires state/get (cross-axis is fine).
	write("ok", hdr("strategy", "ok")+"provides:\n  - capability: rat://strategy/v1/apply\nrequires:\n  - capability: rat://state/v1/get\n")
	if err := runPluginCheck([]string{"ok"}, &out); err != nil {
		t.Fatalf("valid plugin should pass: %v", err)
	}

	// a made-up `requires` in a LINKED axis → fail (not just a syntax check).
	write("bogus", hdr("strategy", "bogus")+"provides:\n  - capability: rat://strategy/v1/apply\nrequires:\n  - capability: rat://state/v1/nonsense\n")
	if err := runPluginCheck([]string{"bogus"}, &out); err == nil {
		t.Error("a made-up requires in a linked axis should fail")
	}

	// kind/axis mismatch: a state-backend providing a strategy capability → fail.
	write("mismatch", hdr("state-backend", "mismatch")+"provides:\n  - capability: rat://strategy/v1/apply\n")
	if err := runPluginCheck([]string{"mismatch"}, &out); err == nil {
		t.Error("a state-backend providing a strategy cap should fail kind coherence")
	}

	// ADR-039 authoring gate: neither provides nor requires → fail (a plugin must do something).
	write("empty", hdr("ui", "empty"))
	if err := runPluginCheck([]string{"empty"}, &out); err == nil {
		t.Error("a manifest with no provides and no requires should fail (ADR-039)")
	}

	// ADR-039 authoring gate: a driver (provides:[] but requires≥1) PASSES.
	write("driver", hdr("ui", "driver")+"requires:\n  - capability: rat://strategy/v1/apply\n")
	if err := runPluginCheck([]string{"driver"}, &out); err != nil {
		t.Errorf("a driver (provides:[] + requires) should pass: %v", err)
	}

	// Gap 3: a manifest missing resources fails the authoring gate.
	write("nores", "api_version: rat/1\nkind: strategy\nmetadata:\n  name: nores\n  version: 0.1.0\ncompatible_core: [\"rat/1\"]\nprovides:\n  - capability: rat://strategy/v1/apply\n")
	if err := runPluginCheck([]string{"nores"}, &out); err == nil {
		t.Error("a manifest missing resources.requests should fail the authoring gate (Gap 3)")
	}
}

// TestResolveMethod covers the capability→gRPC-method resolution `rat plugin test` uses to
// smoke-invoke a launched plugin (no podman needed — just the linked descriptors).
func TestResolveMethod(t *testing.T) {
	path, in, out, err := resolveMethod("rat://secret/v1/resolve")
	if err != nil {
		t.Fatalf("resolveMethod: %v", err)
	}
	if path != "/rat.secret.v1.SecretService/Resolve" {
		t.Errorf("path = %q, want /rat.secret.v1.SecretService/Resolve", path)
	}
	if in == nil || out == nil {
		t.Error("expected non-nil input/output descriptors")
	}
	if _, _, _, err := resolveMethod("rat://nope/v1/none"); err == nil {
		t.Error("expected error for an unknown capability")
	}
}
