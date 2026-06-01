package conformance

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func mustKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// TestVerifyAcceptsGenuineAttestation: a signature from the authority's key verifies,
// and Conforms reflects the attested set (order-independent — canonical form sorts).
func TestVerifyAcceptsGenuineAttestation(t *testing.T) {
	pub, priv := mustKey(t)
	au := NewAuthority(map[string]ed25519.PublicKey{"k1": pub})

	att := Sign(priv, "k1", "rat-fmt", []string{"rat://format/v1/scan", "rat://format/v1/overwrite"})
	if err := au.Verify(att); err != nil {
		t.Fatalf("Verify genuine = %v, want nil", err)
	}
	if !att.Conforms("rat://format/v1/overwrite") || !att.Conforms("rat://format/v1/scan") {
		t.Error("Conforms should report both attested capabilities")
	}
	if att.Conforms("rat://format/v1/merge") {
		t.Error("Conforms should be false for an unattested capability")
	}
}

// TestVerifyRejectsWrongKey: an attestation signed by a key the authority does not
// publish for that key id does not verify (key-substitution defense — the signature
// commits to the key id).
func TestVerifyRejectsWrongKey(t *testing.T) {
	pub, _ := mustKey(t)
	_, rogue := mustKey(t)
	au := NewAuthority(map[string]ed25519.PublicKey{"k1": pub})

	forged := Sign(rogue, "k1", "rat-fmt", []string{"rat://format/v1/overwrite"})
	if err := au.Verify(forged); err == nil {
		t.Fatal("Verify of a rogue-signed attestation = nil, want error")
	}
}

// TestVerifyRejectsTampering: mutating the conformed set after signing breaks the
// signature (the plugin can't widen its conformed capabilities).
func TestVerifyRejectsTampering(t *testing.T) {
	pub, priv := mustKey(t)
	au := NewAuthority(map[string]ed25519.PublicKey{"k1": pub})

	att := Sign(priv, "k1", "rat-fmt", []string{"rat://format/v1/overwrite"})
	att.Conformed = append(att.Conformed, "rat://format/v1/merge") // forge an extra capability
	if err := au.Verify(att); err == nil {
		t.Fatal("Verify of a tampered attestation = nil, want error")
	}
}

// TestVerifyRejectsUnknownKeyID: a key id the authority's keyring doesn't carry.
func TestVerifyRejectsUnknownKeyID(t *testing.T) {
	pub, priv := mustKey(t)
	au := NewAuthority(map[string]ed25519.PublicKey{"k1": pub})

	att := Sign(priv, "k-unknown", "rat-fmt", []string{"rat://format/v1/overwrite"})
	if err := au.Verify(att); err == nil {
		t.Fatal("Verify with an unknown key_id = nil, want error")
	}
}
