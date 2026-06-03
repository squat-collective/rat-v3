package main

// signing.go — ed25519 provenance for marketplace indexes (the house signature algo, seeded
// by D4's attestation). A publisher signs the index BYTES with their private key, producing a
// detached `<index>.sig` (base64). A consumer pins the publisher's public key when adding the
// source (`rat marketplace add … --pubkey`); rat verifies every fetch (and the cached copy on
// offline fallback). Keys/signatures are base64(std) — ed25519: 32-byte public, 64-byte
// private, 64-byte signature.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// genKeypair returns a fresh ed25519 (publicB64, privateB64) keypair.
func genKeypair() (pub, priv string, err error) {
	p, s, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(p), base64.StdEncoding.EncodeToString(s), nil
}

// signBytes signs data with a base64 ed25519 private key, returning a base64 signature.
func signBytes(privB64 string, data []byte) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privB64))
	if err != nil {
		return "", fmt.Errorf("decode private key: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(raw))
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(raw), data)), nil
}

// verifyBytes checks a base64 ed25519 signature over data against a base64 public key.
func verifyBytes(pubB64 string, data []byte, sigB64 string) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubB64))
	if err != nil {
		return fmt.Errorf("decode public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("public key must be %d bytes, got %d", ed25519.PublicKeySize, len(pub))
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return fmt.Errorf("signature does not match (wrong key or tampered index)")
	}
	return nil
}

// resolveKeyArg accepts either a base64 key literal or a path to a file holding one.
func resolveKeyArg(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if b, err := os.ReadFile(v); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(v), nil
}
