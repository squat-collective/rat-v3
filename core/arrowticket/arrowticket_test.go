package arrowticket

import (
	"testing"
	"time"
)

func TestValidThenSingleUse(t *testing.T) {
	m := NewMinter([]byte("secret-key"))
	tk, err := m.Mint("stream-1", "rat-strategy", "tenantA", time.Minute)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if err := m.Validate(tk, "stream-1", "rat-strategy", "tenantA"); err != nil {
		t.Fatalf("valid ticket rejected: %v", err)
	}
	// Single-use: the second presentation of the same ticket is rejected.
	if err := m.Validate(tk, "stream-1", "rat-strategy", "tenantA"); err != ErrReplay {
		t.Fatalf("replay err = %v, want ErrReplay", err)
	}
}

func TestBindingEnforced(t *testing.T) {
	m := NewMinter([]byte("k"))
	mint := func() []byte {
		tk, _ := m.Mint("stream-1", "rat-strategy", "tenantA", time.Minute)
		return tk
	}
	// Each fresh ticket must fail when presented with a mismatched binding.
	if err := m.Validate(mint(), "stream-1", "rat-strategy", "tenantB"); err != ErrNotBound {
		t.Errorf("cross-tenant err = %v, want ErrNotBound", err)
	}
	if err := m.Validate(mint(), "stream-1", "rat-OTHER", "tenantA"); err != ErrNotBound {
		t.Errorf("cross-caller err = %v, want ErrNotBound", err)
	}
	if err := m.Validate(mint(), "stream-OTHER", "rat-strategy", "tenantA"); err != ErrNotBound {
		t.Errorf("cross-stream err = %v, want ErrNotBound", err)
	}
}

func TestExpiry(t *testing.T) {
	m := NewMinter([]byte("k"))
	tk, _ := m.Mint("s", "c", "t", time.Millisecond)
	m.now = func() time.Time { return time.Now().Add(time.Hour) } // advance the clock past expiry
	if err := m.Validate(tk, "s", "c", "t"); err != ErrExpired {
		t.Fatalf("expired err = %v, want ErrExpired", err)
	}
}

func TestTamperOrWrongKey(t *testing.T) {
	m := NewMinter([]byte("real-key"))
	tk, _ := m.Mint("s", "c", "t", time.Minute)
	// A validator with a different key cannot accept the ticket (forgery/tamper).
	other := NewMinter([]byte("different-key"))
	if err := other.Validate(tk, "s", "c", "t"); err != ErrTampered {
		t.Fatalf("wrong-key err = %v, want ErrTampered", err)
	}
	// Garbage bytes are malformed, not a panic.
	if err := m.Validate([]byte("not-json"), "s", "c", "t"); err != ErrMalformed {
		t.Errorf("malformed err = %v, want ErrMalformed", err)
	}
}
