package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR-050: plane files interpolate ${VAR} from the process env — braced form only,
// fail-loud on undefined vars — so the platform fact sheet lives once (platform/.env)
// instead of being inlined into every file.

func TestExpandEnvRefs(t *testing.T) {
	t.Setenv("ADR050_PASS", "s3cret")
	for in, want := range map[string]string{
		"host=db password=${ADR050_PASS}": "host=db password=s3cret",
		"$${ADR050_PASS} stays":           "${ADR050_PASS} stays", // escape
		"bare $DOLLAR untouched":          "bare $DOLLAR untouched",
		"no refs":                         "no refs",
		"${ADR050_PASS}${ADR050_PASS}":    "s3crets3cret",
	} {
		got, err := expandEnvRefs(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q → %q, want %q", in, got, want)
		}
	}

	if _, err := expandEnvRefs("x ${ADR050_NOPE} y"); err == nil || !strings.Contains(err.Error(), "${ADR050_NOPE}") {
		t.Fatalf("undefined var must error with its name, got %v", err)
	}
	if _, err := expandEnvRefs("x ${unterminated"); err == nil {
		t.Fatal("unterminated ref must error")
	}
}

func TestPlaneInterpolatesEnvRefs(t *testing.T) {
	t.Setenv("ADR050_DSN", "host=pg user=u password=p")
	t.Setenv("ADR050_EP", "127.0.0.1:50099")

	dir := t.TempDir()
	vWrite(t, filepath.Join(dir, "state.plugin.yaml"), goodState)
	vWrite(t, filepath.Join(dir, "plane.yaml"), `
runtime: podman
plugins:
  - name: state
    manifest: ./state.plugin.yaml
    launch:
      image: rat/state:dev
      env: { RAT_STATE_PG: "${ADR050_DSN}", PLAIN: literal }
`)
	pl, err := LoadPlane(filepath.Join(dir, "plane.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	env := pl.Specs[0].Launch.Env
	if env["RAT_STATE_PG"] != "host=pg user=u password=p" || env["PLAIN"] != "literal" {
		t.Fatalf("env not interpolated: %v", env)
	}

	vWrite(t, filepath.Join(dir, "attach.yaml"), `
runtime: local
plugins:
  - name: state
    manifest: ./state.plugin.yaml
    endpoint: "${ADR050_EP}"
`)
	pl, err = LoadPlane(filepath.Join(dir, "attach.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if pl.Specs[0].Endpoint != "127.0.0.1:50099" {
		t.Fatalf("endpoint not interpolated: %q", pl.Specs[0].Endpoint)
	}
}

func TestPlaneUndefinedVarFailsLoud(t *testing.T) {
	os.Unsetenv("ADR050_MISSING")
	dir := t.TempDir()
	vWrite(t, filepath.Join(dir, "state.plugin.yaml"), goodState)
	vWrite(t, filepath.Join(dir, "plane.yaml"), `
runtime: podman
plugins:
  - name: state
    manifest: ./state.plugin.yaml
    launch:
      image: rat/state:dev
      env: { DSN: "${ADR050_MISSING}" }
`)
	_, err := LoadPlane(filepath.Join(dir, "plane.yaml"))
	if err == nil || !strings.Contains(err.Error(), "${ADR050_MISSING}") {
		t.Fatalf("want the named-var load error, got %v", err)
	}
}
