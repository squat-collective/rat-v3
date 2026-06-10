package registry

import (
	"testing"

	"github.com/rat-dev/rat/core/manifest"
)

// exampleManifestsDir is the repo's frozen example manifests, relative to this
// package (core/registry -> repo root -> contracts/examples).
const exampleManifestsDir = "../../contracts/examples"

// TestAuthorizeAgainstRealManifests is the heart of the C5 spike: load the REAL
// frozen manifests and prove the authorization decision is derived from their
// declared provides/requires — not from any allowlist.
func TestAuthorizeAgainstRealManifests(t *testing.T) {
	manifests, err := manifest.LoadDir(exampleManifestsDir)
	if err != nil {
		t.Fatalf("LoadDir(%s): %v", exampleManifestsDir, err)
	}
	if len(manifests) != 2 {
		t.Fatalf("expected 2 example manifests (scd2 strategy, delta format), got %d", len(manifests))
	}

	reg, err := New(manifests)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Provider index is built from the manifests' `provides` lists.
	if got := reg.ProviderOf("rat://format/v1/merge"); got != "rat-format-deltalake" {
		t.Errorf("ProviderOf(format/merge) = %q, want rat-format-deltalake", got)
	}
	if got := reg.ProviderOf("rat://strategy/v1/apply"); got != "rat-strategy-scd2" {
		t.Errorf("ProviderOf(strategy/apply) = %q, want rat-strategy-scd2", got)
	}

	tests := []struct {
		name        string
		caller      string
		capURI      string
		wantAllowed bool
		wantProv    string
	}{
		{
			// scd2 declares `requires: format/merge`; delta `provides` it. Allow.
			name: "allow: declared requires + a real provider",
			caller: "rat-strategy-scd2", capURI: "rat://format/v1/merge",
			wantAllowed: true, wantProv: "rat-format-deltalake",
		},
		{
			// delta provides format/scan, but scd2 does NOT declare requiring it.
			// The capability existing is not enough — the caller must DECLARE it.
			name: "deny: provider exists but caller did not declare requires",
			caller: "rat-strategy-scd2", capURI: "rat://format/v1/scan",
			wantAllowed: false,
		},
		{
			// scd2 requires runtime/execute, but no runtime plugin is registered.
			name: "deny: declared requires but no provider registered",
			caller: "rat-strategy-scd2", capURI: "rat://runtime/v1/execute",
			wantAllowed: false,
		},
		{
			// delta requires storage/vend-credentials; no storage plugin registered.
			name: "deny: declared requires but no provider registered (format->storage)",
			caller: "rat-format-deltalake", capURI: "rat://storage/v1/vend-credentials",
			wantAllowed: false,
		},
		{
			name: "deny: unknown caller",
			caller: "rat-ghost-plugin", capURI: "rat://format/v1/merge",
			wantAllowed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := reg.Authorize(tc.caller, tc.capURI)
			if d.Allowed != tc.wantAllowed {
				t.Fatalf("Authorize(%q, %q).Allowed = %v (%s), want %v",
					tc.caller, tc.capURI, d.Allowed, d.Reason, tc.wantAllowed)
			}
			if tc.wantAllowed && d.Provider != tc.wantProv {
				t.Errorf("Authorize(%q, %q).Provider = %q, want %q",
					tc.caller, tc.capURI, d.Provider, tc.wantProv)
			}
			if d.Reason == "" {
				t.Errorf("Authorize(%q, %q).Reason is empty (needed for the audit record)", tc.caller, tc.capURI)
			}
		})
	}
}

// TestCoexistingProvidersSelectedByLabel (ADR-045): two providers of ONE capability coexist,
// each labeled; a call's selector picks one deterministically. No selector (ambiguous) and a
// non-matching selector both fail CLOSED — authorized but not allowed, never resolved arbitrarily.
func TestCoexistingProvidersSelectedByLabel(t *testing.T) {
	small := &manifest.Manifest{
		Kind: "engine", Metadata: manifest.Metadata{Name: "engine-duckdb", Labels: map[string]string{"compute": "small"}},
		Provides: []manifest.CapabilityRef{{Capability: "rat://engine/v1/execute"}},
	}
	big := &manifest.Manifest{
		Kind: "engine", Metadata: manifest.Metadata{Name: "engine-spark", Labels: map[string]string{"compute": "big"}},
		Provides: []manifest.CapabilityRef{{Capability: "rat://engine/v1/execute"}},
	}
	caller := &manifest.Manifest{
		Kind: "strategy", Metadata: manifest.Metadata{Name: "flow"},
		Requires: []manifest.CapabilityRef{{Capability: "rat://engine/v1/execute"}},
	}
	reg, err := New([]*manifest.Manifest{small, big, caller})
	if err != nil {
		t.Fatalf("New rejected two coexisting providers of one capability: %v", err)
	}
	if got := reg.ProvidersOf("rat://engine/v1/execute"); len(got) != 2 {
		t.Fatalf("ProvidersOf = %v, want both providers", got)
	}

	// A selector picks the matching provider.
	if d := reg.Select("flow", "rat://engine/v1/execute", map[string]string{"compute": "big"}); !d.Allowed || d.Provider != "engine-spark" {
		t.Errorf("Select(compute=big) = %+v, want allowed via engine-spark", d)
	}
	if d := reg.Select("flow", "rat://engine/v1/execute", map[string]string{"compute": "small"}); !d.Allowed || d.Provider != "engine-duckdb" {
		t.Errorf("Select(compute=small) = %+v, want allowed via engine-duckdb", d)
	}

	// No selector → ambiguous → fail closed (authorized, not allowed).
	if d := reg.Select("flow", "rat://engine/v1/execute", nil); d.Allowed || !d.Authorized {
		t.Errorf("Select(no selector) = %+v, want authorized-but-not-allowed (ambiguous)", d)
	}
	// A selector matching no provider → fail closed.
	if d := reg.Select("flow", "rat://engine/v1/execute", map[string]string{"compute": "gpu"}); d.Allowed || !d.Authorized {
		t.Errorf("Select(compute=gpu) = %+v, want authorized-but-not-allowed (no match)", d)
	}
}

// TestNewAllowsDuplicateProviderName still rejects a duplicate plugin NAME (the one invariant
// that stays).
func TestNewAllowsDuplicateProviderName(t *testing.T) {
	a := &manifest.Manifest{
		Kind: "format", Metadata: manifest.Metadata{Name: "dup"},
		Provides: []manifest.CapabilityRef{{Capability: "rat://format/v1/scan"}},
	}
	b := &manifest.Manifest{
		Kind: "format", Metadata: manifest.Metadata{Name: "dup"},
		Provides: []manifest.CapabilityRef{{Capability: "rat://format/v1/overwrite"}},
	}
	if _, err := New([]*manifest.Manifest{a, b}); err == nil {
		t.Fatal("New accepted two plugins with the same name; want an error")
	}
}

// TestManifestValidationRejectsMalformedURI guards the URI grammar at load time.
func TestManifestValidationRejectsMalformedURI(t *testing.T) {
	m := &manifest.Manifest{
		Kind: "format", Metadata: manifest.Metadata{Name: "fmt-bad"},
		Provides: []manifest.CapabilityRef{{Capability: "format/scan"}}, // missing rat:// + version
	}
	if err := m.Validate(); err == nil {
		t.Fatal("Validate accepted a malformed capability URI; want an error")
	}
	if !manifest.ValidCapabilityURI("rat://format/v1/scan") {
		t.Error("ValidCapabilityURI rejected a well-formed URI")
	}
}
