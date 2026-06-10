package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The preflight (DX-1) must catch, BEFORE boot, exactly the failures that today surface
// as warnings + Degraded minutes into a run: typo'd capabilities, requires without a
// provider, unlaunchable images, mixed modes, duplicate names — and must NOT flag the
// open-set case (capabilities of axes this rat doesn't link) as an error.

func vWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// vPlane writes a plane + manifests into a temp dir and loads it.
func vPlane(t *testing.T, planeYAML string, manifests map[string]string) *Plane {
	t.Helper()
	dir := t.TempDir()
	for name, content := range manifests {
		vWrite(t, filepath.Join(dir, name), content)
	}
	pp := filepath.Join(dir, "plane.yaml")
	vWrite(t, pp, planeYAML)
	pl, err := LoadPlane(pp)
	if err != nil {
		t.Fatalf("LoadPlane: %v", err)
	}
	return pl
}

func issuesByLevel(issues []vIssue) (errs, warns []string) {
	for _, is := range issues {
		if is.err {
			errs = append(errs, is.msg)
		} else {
			warns = append(warns, is.msg)
		}
	}
	return
}

func wantContains(t *testing.T, haystack []string, needle string) {
	t.Helper()
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return
		}
	}
	t.Fatalf("no finding contains %q; got %q", needle, haystack)
}

const goodState = `api_version: rat/1
kind: state-backend
metadata: {name: state, version: 0.1.0}
provides:
  - capability: rat://state/v1/get
  - capability: rat://state/v1/put
  - capability: rat://state/v1/list
resources:
  requests: {cpu: "50m", memory: "64Mi"}
`

const devDriver = `api_version: rat/1
kind: state-backend
metadata: {name: dev, version: 0.1.0}
provides: []
requires:
  - capability: rat://state/v1/get
`

func TestPreflightHappyPlane(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "stateplugin")
	vWrite(t, bin, "#!/bin/sh\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	pl := vPlane(t, `
runtime: local
plugins:
  - name: state
    manifest: ./state.plugin.yaml
    launch: { image: `+bin+` }
  - name: dev
    manifest: ./dev.plugin.yaml
`, map[string]string{"state.plugin.yaml": goodState, "dev.plugin.yaml": devDriver})

	errs, warns := issuesByLevel(preflight(pl, defaultImageProbe))
	if len(errs) != 0 || len(warns) != 0 {
		t.Fatalf("want clean preflight, got errs=%q warns=%q", errs, warns)
	}
}

func TestPreflightCatchesTheDegradedBootFailures(t *testing.T) {
	// One plane, several latent failures: a typo'd capability, an unsatisfied
	// requires, a missing launch binary, a provider without resources.
	pl := vPlane(t, `
runtime: local
plugins:
  - name: state
    manifest: ./state.plugin.yaml
    launch: { image: ./does-not-exist }
`, map[string]string{"state.plugin.yaml": `api_version: rat/1
kind: state-backend
metadata: {name: state, version: 0.1.0}
provides:
  - capability: rat://state/v1/get
  - capability: rat://state/v1/puttt
requires:
  - capability: rat://secret/v1/resolve
`})

	errs, warns := issuesByLevel(preflight(pl, defaultImageProbe))
	wantContains(t, errs, `"rat://state/v1/puttt" is not a real capability`)
	wantContains(t, errs, "requires rat://secret/v1/resolve — no provider")
	wantContains(t, errs, "not found")
	wantContains(t, warns, "no resources.requests")
	if len(errs) != 3 {
		t.Fatalf("want exactly 3 errors, got %d: %q", len(errs), errs)
	}
}

func TestPreflightOpenSetAxisIsWarningNotError(t *testing.T) {
	// A kind with no well-known axis providing/requiring capabilities of an axis this
	// rat does not link: unverified (warning), never an error — the open-set promise.
	pl := vPlane(t, `
runtime: local
plugins:
  - name: custom
    manifest: ./custom.plugin.yaml
`, map[string]string{"custom.plugin.yaml": `api_version: rat/1
kind: customaxis
metadata: {name: custom, version: 0.1.0}
provides:
  - capability: rat://myaxis/v1/frob
requires:
  - capability: rat://myaxis/v1/frob
`})

	errs, warns := issuesByLevel(preflight(pl, defaultImageProbe))
	if len(errs) != 0 {
		t.Fatalf("open-set capabilities must not be errors, got %q", errs)
	}
	wantContains(t, warns, "isn't compiled into this rat")
}

func TestPreflightMixedModeAndDuplicateNames(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "p")
	vWrite(t, bin, "#!/bin/sh\n")
	_ = os.Chmod(bin, 0o755)
	pl := vPlane(t, `
runtime: local
plugins:
  - name: state
    manifest: ./state.plugin.yaml
    launch: { image: `+bin+` }
  - name: state
    manifest: ./state.plugin.yaml
    endpoint: 127.0.0.1:50051
`, map[string]string{"state.plugin.yaml": goodState})

	errs, _ := issuesByLevel(preflight(pl, defaultImageProbe))
	wantContains(t, errs, `duplicate plugin name "state"`)
	wantContains(t, errs, "mixes launch and attach")
}

func TestRunValidateRendersAndFailsOnErrors(t *testing.T) {
	dir := t.TempDir()
	vWrite(t, filepath.Join(dir, "state.plugin.yaml"), goodState)
	vWrite(t, filepath.Join(dir, "plane.yaml"), `
runtime: local
plugins:
  - name: state
    manifest: ./state.plugin.yaml
    launch: { image: ./missing-binary }
`)
	var out strings.Builder
	err := runValidate([]string{"--plane", filepath.Join(dir, "plane.yaml")}, &out)
	if err == nil || !strings.Contains(err.Error(), "preflight error") {
		t.Fatalf("want a preflight-error return, got %v", err)
	}
	for _, want := range []string{"rat validate —", "✗", "will NOT come up"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunValidateHappyOutput(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "stateplugin")
	vWrite(t, bin, "#!/bin/sh\n")
	_ = os.Chmod(bin, 0o755)
	vWrite(t, filepath.Join(dir, "state.plugin.yaml"), goodState)
	vWrite(t, filepath.Join(dir, "plane.yaml"), `
runtime: local
plugins:
  - name: state
    manifest: ./state.plugin.yaml
    launch: { image: ./stateplugin }
`)
	var out strings.Builder
	if err := runValidate([]string{"--plane", filepath.Join(dir, "plane.yaml")}, &out); err != nil {
		t.Fatalf("runValidate: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "✓ launchable as declared") {
		t.Fatalf("want the all-clear verdict, got:\n%s", out.String())
	}
}

func TestRunValidateNothingToValidate(t *testing.T) {
	t.Chdir(t.TempDir())
	var out strings.Builder
	err := runValidate(nil, &out)
	if err == nil || !strings.Contains(err.Error(), "nothing to validate") {
		t.Fatalf("want the nothing-to-validate error, got %v", err)
	}
}
