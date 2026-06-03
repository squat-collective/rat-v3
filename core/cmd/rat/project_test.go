package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectInitAddLoad covers the poetry loop (ADR-023): `rat init` writes a rat.toml
// shell, `rat add` appends a [[plugin]], and LoadProject reduces it to a Plane with the
// per-project unix-socket default + the instance id — all without a daemon.
func TestProjectInitAddLoad(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.MkdirAll("manifests", 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "api_version: rat/1\nkind: state-backend\nmetadata:\n  name: rat-state\n  version: 0.1.0\ncompatible_core: [\"rat/1\"]\nprovides:\n  - capability: rat://state/v1/get\n"
	if err := os.WriteFile(filepath.Join("manifests", "state.plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runInit([]string{"--name", "demo", "--runtime", "local"}, &out); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat("rat.toml"); err != nil {
		t.Fatalf("rat.toml not written: %v", err)
	}
	if err := runAdd([]string{"rat-state", "--image", "/bin/true", "--manifest", "manifests/state.plugin.yaml"}, &out); err != nil {
		t.Fatalf("add: %v", err)
	}

	pl, err := LoadProject("rat.toml")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if pl.Instance != "demo" {
		t.Errorf("instance = %q, want demo", pl.Instance)
	}
	if !strings.HasPrefix(pl.Addr, "unix:") || !strings.Contains(pl.Addr, filepath.Join(".rat", "daemon.sock")) {
		t.Errorf("addr = %q, want a per-project unix socket", pl.Addr)
	}
	if len(pl.Specs) != 1 || pl.Specs[0].Manifest.Metadata.Name != "rat-state" {
		t.Fatalf("specs = %+v, want one rat-state", pl.Specs)
	}
	if pl.Specs[0].Launch == nil || pl.Specs[0].Launch.Image == "" {
		t.Errorf("expected a launch spec with an image")
	}

	// `rat add` of a duplicate name is rejected.
	if err := runAdd([]string{"rat-state", "--manifest", "manifests/state.plugin.yaml"}, &out); err == nil {
		t.Errorf("expected duplicate add to be rejected")
	}
	// `rat init` refuses to clobber an existing project.
	if err := runInit(nil, &out); err == nil {
		t.Errorf("expected init to refuse an existing rat.toml")
	}
}
