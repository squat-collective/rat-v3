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

// TestNewRejectsDuplicateProvider proves the core refuses ambiguity rather than
// picking a provider arbitrarily (the spike has no selection policy).
func TestNewRejectsDuplicateProvider(t *testing.T) {
	a := &manifest.Manifest{
		Kind: "format", Metadata: manifest.Metadata{Name: "fmt-a"},
		Provides: []manifest.CapabilityRef{{Capability: "rat://format/v1/scan"}},
	}
	b := &manifest.Manifest{
		Kind: "format", Metadata: manifest.Metadata{Name: "fmt-b"},
		Provides: []manifest.CapabilityRef{{Capability: "rat://format/v1/scan"}},
	}
	if _, err := New([]*manifest.Manifest{a, b}); err == nil {
		t.Fatal("New accepted two providers of the same capability; want an error")
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
