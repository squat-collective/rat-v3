package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemovePluginBlock: removing one [[plugin]] block leaves the header comments and the
// sibling blocks (including a [plugin.env] sub-table) verbatim, and re-parses cleanly.
func TestRemovePluginBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rat.toml")
	const start = `# my project — command-written, do not hand-edit
name    = "demo"
runtime = "podman"

[[plugin]]
name     = "rat-secret"
image    = "ghcr.io/x/secret:1"
manifest = "manifests/rat-secret.plugin.yaml"
isolation = "i9"

[[plugin]]
name     = "rat-state"
image    = "ghcr.io/x/state:1"
manifest = "manifests/rat-state.plugin.yaml"
isolation = "i9"
[plugin.env]
RAT_STATE_PG_REF = "ref://state/pg-dsn"

[[plugin]]
name     = "dbt-runner"
image    = "ghcr.io/x/dbt:1"
manifest = "manifests/dbt-runner.plugin.yaml"
isolation = "i9"
`
	if err := os.WriteFile(path, []byte(start), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := removePluginBlock(path, "rat-state"); err != nil {
		t.Fatalf("removePluginBlock: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)

	if strings.Contains(s, "rat-state") || strings.Contains(s, "RAT_STATE_PG_REF") {
		t.Fatalf("rat-state block (incl. its [plugin.env]) should be gone:\n%s", s)
	}
	for _, want := range []string{"# my project", `name    = "demo"`, "rat-secret", "dbt-runner"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q preserved:\n%s", want, s)
		}
	}

	// the file must still parse, with exactly the two survivors.
	rt, err := parseProject(path)
	if err != nil {
		t.Fatalf("re-parse after remove: %v", err)
	}
	if len(rt.Plugins) != 2 {
		t.Fatalf("want 2 plugins after remove, got %d", len(rt.Plugins))
	}

	// removing something absent is an error, not a silent no-op.
	if err := removePluginBlock(path, "nope"); err == nil {
		t.Fatal("removing an absent plugin should error")
	}
}
