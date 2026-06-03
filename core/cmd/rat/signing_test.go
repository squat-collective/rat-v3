package main

import "testing"

// TestSignVerifyRoundTrip: a signature from the matching key verifies; a tampered payload,
// a wrong key, and a garbage signature all fail. This is the trust contract the marketplace
// relies on (`rat marketplace sign` → pinned-key verify on fetch).
func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := genKeypair()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	payload := []byte(`{"name":"official","plugins":[]}`)

	sig, err := signBytes(priv, payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := verifyBytes(pub, payload, sig); err != nil {
		t.Fatalf("verify (happy path) should pass, got %v", err)
	}

	// tampered payload → reject
	if err := verifyBytes(pub, []byte(`{"name":"evil","plugins":[]}`), sig); err == nil {
		t.Fatal("verify should FAIL on a tampered payload")
	}

	// wrong key → reject
	otherPub, _, _ := genKeypair()
	if err := verifyBytes(otherPub, payload, sig); err == nil {
		t.Fatal("verify should FAIL against a non-matching key")
	}

	// garbage signature → reject (decode/length error)
	if err := verifyBytes(pub, payload, "not-base64-!!"); err == nil {
		t.Fatal("verify should FAIL on an undecodable signature")
	}
}
