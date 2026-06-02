// Package conformance is the core's D4 enforcement primitive: a verifiable proof
// that a plugin CONFORMED a set of capabilities (passed their golden-data vectors),
// so the core can require `declared == conformed` instead of trusting a manifest's
// self-asserted `provides` (reviews/10 D4; marketplace.proto conformed_capabilities).
//
// "Capability declared" is meaningless without "capability conformed" — that is what
// stops capability negotiation from being a lie (format/v1 CONTRACT C6). An
// Attestation is authored + signed by a conformance authority (the marketplace /
// conformance runner); the core verifies it against the authority's PUBLISHED key
// before trusting the plugin. A plugin therefore cannot forge its own conformed set.
//
// This is the core's first real signature verification — the spike's audit record +
// isolation receipt are unsigned (signing deferred to GA); D4 brings ed25519 in, and
// the keyID model here is the seed for the C4 audit-signing + C8 supply-chain keyring.
package conformance

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Attestation is a conformance authority's signed statement that PluginName passed
// conformance for the Conformed capability URIs. Signature covers the canonical form
// (which INCLUDES KeyID, so the signature commits to which key it claims to be from —
// defeating key-substitution, mirroring the audit record's key_id).
type Attestation struct {
	PluginName string
	Conformed  []string // capability URIs the plugin passed conformance for
	KeyID      string   // selects the authority key in the core's keyring
	// ExpiresAtUnixMs bounds the attestation's validity (Q02 PU-3, ADR-017): a
	// conformed-then-CVE'd plugin must not be "conformed forever". 0 == no expiry.
	// INCLUDED in the signed canonical bytes, so expiry cannot be tampered/extended.
	ExpiresAtUnixMs int64
	Signature       []byte
}

// canonicalBytes is the deterministic signed form: plugin name, then the conformed
// capabilities SORTED (so signature is order-independent) and newline-joined, then
// the key id, then the expiry — each separated by a NUL that can't appear in a
// capability URI. Including ExpiresAtUnixMs (Q02 PU-3) makes the expiry tamper-evident.
func (a Attestation) canonicalBytes() []byte {
	caps := append([]string(nil), a.Conformed...)
	sort.Strings(caps)
	return []byte(a.PluginName + "\x00" + strings.Join(caps, "\n") + "\x00" + a.KeyID + "\x00" + strconv.FormatInt(a.ExpiresAtUnixMs, 10))
}

// Conforms reports whether capURI is in the attested conformed set.
func (a Attestation) Conforms(capURI string) bool {
	for _, c := range a.Conformed {
		if c == capURI {
			return true
		}
	}
	return false
}

// Sign produces a signed Attestation for (plugin, conformed) from an authority key.
// Test/tooling helper — in production the conformance runner signs after a plugin
// passes its golden vectors.
func Sign(priv ed25519.PrivateKey, keyID, plugin string, conformed []string) Attestation {
	return SignWithExpiry(priv, keyID, plugin, conformed, 0) // 0 == never expires
}

// SignWithExpiry is Sign with an expiry bound (Q02 PU-3, ADR-017): the attestation is
// valid only until expiresAtUnixMs (0 == never). Expiry is part of the signed bytes, so
// a plugin cannot extend its own conformance lifetime.
func SignWithExpiry(priv ed25519.PrivateKey, keyID, plugin string, conformed []string, expiresAtUnixMs int64) Attestation {
	a := Attestation{PluginName: plugin, Conformed: append([]string(nil), conformed...), KeyID: keyID, ExpiresAtUnixMs: expiresAtUnixMs}
	a.Signature = ed25519.Sign(priv, a.canonicalBytes())
	return a
}

// Authority is the core's trusted conformance keyring: key id -> public key. A real
// deployment publishes it; the spike uses a test authority. Rotation + algorithm
// agility ride on new key ids (a verifier picks the right key without out-of-band
// agreement) — the GA model, mirroring common/v1.AuditRecord.key_id.
type Authority struct {
	keys    map[string]ed25519.PublicKey
	revoked map[string]bool // plugin\x00capURI -> revoked (Q02 PU-3)
	now     func() int64    // unix ms; injectable for tests, real clock by default
}

// NewAuthority builds an authority over a key id -> public key map.
func NewAuthority(keys map[string]ed25519.PublicKey) *Authority {
	return &Authority{keys: keys, revoked: map[string]bool{}, now: func() int64 { return time.Now().UnixMilli() }}
}

func revokeKey(plugin, capURI string) string { return plugin + "\x00" + capURI }

// Revoke withdraws conformance for (plugin, capURI) regardless of any signed
// attestation (Q02 PU-3, ADR-017). The out-of-band revocation channel — a CRL /
// transparency log, surfaced on marketplace.proto Listing.revoked_capabilities — feeds
// this, so a conformed-then-vulnerable capability can be pulled WITHOUT rotating the
// authority key (which would invalidate every other plugin under it).
func (au *Authority) Revoke(plugin, capURI string) { au.revoked[revokeKey(plugin, capURI)] = true }

// IsRevoked reports whether conformance for (plugin, capURI) has been revoked.
func (au *Authority) IsRevoked(plugin, capURI string) bool { return au.revoked[revokeKey(plugin, capURI)] }

// Verify checks the attestation's signature against the authority key named by its
// KeyID. An unknown key id or a signature that does not verify is an error — the
// authority of the record, not the sink's trust.
func (au *Authority) Verify(a Attestation) error {
	pub, ok := au.keys[a.KeyID]
	if !ok {
		return fmt.Errorf("unknown conformance key_id %q", a.KeyID)
	}
	if !ed25519.Verify(pub, a.canonicalBytes(), a.Signature) {
		return fmt.Errorf("conformance attestation for %q does not verify against key_id %q", a.PluginName, a.KeyID)
	}
	// Expiry (Q02 PU-3, ADR-017): a conformed-then-CVE'd plugin must not stay trusted
	// forever — an expired attestation does not verify even with a valid signature.
	if a.ExpiresAtUnixMs != 0 && au.now() >= a.ExpiresAtUnixMs {
		return fmt.Errorf("conformance attestation for %q expired (at %d ms)", a.PluginName, a.ExpiresAtUnixMs)
	}
	return nil
}
