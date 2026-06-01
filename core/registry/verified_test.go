package registry

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/rat-dev/rat/core/conformance"
	"github.com/rat-dev/rat/core/manifest"
)

func vcaps(uris ...string) []manifest.CapabilityRef {
	out := make([]manifest.CapabilityRef, len(uris))
	for i, u := range uris {
		out[i] = manifest.CapabilityRef{Capability: u}
	}
	return out
}

// d4Fixture: a format provider (provides overwrite + scan) and a caller (requires
// overwrite, provides nothing), plus a fresh authority keypair.
func d4Fixture(t *testing.T) (*manifest.Manifest, *manifest.Manifest, *conformance.Authority, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	provider := &manifest.Manifest{Kind: "format", Metadata: manifest.Metadata{Name: "rat-fmt"},
		Provides: vcaps("rat://format/v1/overwrite", "rat://format/v1/scan")}
	caller := &manifest.Manifest{Kind: "strategy", Metadata: manifest.Metadata{Name: "rat-strat"},
		Requires: vcaps("rat://format/v1/overwrite")}
	return provider, caller, conformance.NewAuthority(map[string]ed25519.PublicKey{"k1": pub}), priv
}

// TestNewVerifiedAcceptsConformed: a provider whose attestation covers everything it
// declares is accepted, and the resulting registry authorizes the conformed cap
// normally (the gateway's C5 path is unchanged downstream).
func TestNewVerifiedAcceptsConformed(t *testing.T) {
	provider, caller, authority, priv := d4Fixture(t)
	att := conformance.Sign(priv, "k1", "rat-fmt", []string{"rat://format/v1/overwrite", "rat://format/v1/scan"})

	reg, err := NewVerified([]*manifest.Manifest{provider, caller},
		map[string]conformance.Attestation{"rat-fmt": att}, authority)
	if err != nil {
		t.Fatalf("NewVerified (conformed) = %v, want nil", err)
	}
	if d := reg.Authorize("rat-strat", "rat://format/v1/overwrite"); !d.Allowed || d.Provider != "rat-fmt" {
		t.Errorf("Authorize after verification = %+v, want allowed via rat-fmt", d)
	}
}

// TestNewVerifiedRefusesDeclaredButNotConformed: the provider declares `scan` but its
// attestation only covers `overwrite` — declared != conformed → refused.
func TestNewVerifiedRefusesDeclaredButNotConformed(t *testing.T) {
	provider, _, authority, priv := d4Fixture(t)
	partial := conformance.Sign(priv, "k1", "rat-fmt", []string{"rat://format/v1/overwrite"}) // missing scan

	if _, err := NewVerified([]*manifest.Manifest{provider},
		map[string]conformance.Attestation{"rat-fmt": partial}, authority); err == nil {
		t.Fatal("NewVerified accepted a provider declaring an unconformed capability (scan); want refusal")
	}
}

// TestNewVerifiedRefusesForgedSignature: an attestation signed by a key the authority
// does not publish is refused (no self-asserted conformance).
func TestNewVerifiedRefusesForgedSignature(t *testing.T) {
	provider, _, authority, _ := d4Fixture(t)
	_, rogue, _ := ed25519.GenerateKey(rand.Reader)
	forged := conformance.Sign(rogue, "k1", "rat-fmt", []string{"rat://format/v1/overwrite", "rat://format/v1/scan"})

	if _, err := NewVerified([]*manifest.Manifest{provider},
		map[string]conformance.Attestation{"rat-fmt": forged}, authority); err == nil {
		t.Fatal("NewVerified accepted a forged-signature attestation; want refusal")
	}
}

// TestNewVerifiedRefusesMissingAttestation: a provider with no attestation at all is
// refused — unverified is not conformed (marketplace.proto: an empty conformed set is
// "unverified").
func TestNewVerifiedRefusesMissingAttestation(t *testing.T) {
	provider, _, authority, _ := d4Fixture(t)
	if _, err := NewVerified([]*manifest.Manifest{provider},
		map[string]conformance.Attestation{}, authority); err == nil {
		t.Fatal("NewVerified accepted a provider with no attestation; want refusal")
	}
}

// TestNewVerifiedAllowsUnattestedCaller: a pure caller/driver (no `provides`) needs no
// attestation — only providers must conform.
func TestNewVerifiedAllowsUnattestedCaller(t *testing.T) {
	_, caller, authority, _ := d4Fixture(t)
	if _, err := NewVerified([]*manifest.Manifest{caller},
		map[string]conformance.Attestation{}, authority); err != nil {
		t.Fatalf("NewVerified refused a caller that provides nothing: %v", err)
	}
}
