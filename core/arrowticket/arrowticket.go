// Package arrowticket is a reference implementation of the ArrowStream.ticket
// obligation (D2 — common/v1/data.proto SEC-14). The bulk Arrow leg bypasses the
// core, so the ticket is the ONLY gate: a conformant producer MUST issue tickets
// that are short-TTL, single-use, and bound to {caller_plugin, tenant, stream}, so
// a leaked/guessed ticket can't be replayed or used cross-tenant.
//
// This is producer-side (a format plugin hosting a Flight endpoint mints it; the
// consumer presents it at DoGet) — it would ship as an SDK helper, not in the core.
// The spike includes it to probe whether the frozen `bytes ticket` field SUFFICES
// to carry a real, enforceable credential. It does: an opaque HMAC-signed claim set
// + a single-use set covers TTL, binding, single-use, and tamper-resistance with no
// wire change. No freeze-reopen.
package arrowticket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Errors returned by Validate. They are distinct so a producer can audit the
// failure mode (replay vs expiry vs wrong-binding vs tamper).
var (
	ErrMalformed = errors.New("arrowticket: malformed")
	ErrTampered  = errors.New("arrowticket: signature mismatch (tampered or wrong key)")
	ErrExpired   = errors.New("arrowticket: expired")
	ErrNotBound  = errors.New("arrowticket: not bound to this stream/caller/tenant")
	ErrReplay    = errors.New("arrowticket: already used (single-use)")
)

// claims is the binding a ticket asserts.
type claims struct {
	StreamID      string `json:"s"`
	CallerPlugin  string `json:"c"`
	Tenant        string `json:"t"`
	ExpiresUnixMs int64  `json:"e"`
}

type wireTicket struct {
	Claims claims `json:"claims"`
	Sig    []byte `json:"sig"`
}

// Minter mints + validates tickets under one secret key, enforcing single-use.
type Minter struct {
	key  []byte
	now  func() time.Time // injectable clock (tests)
	mu   sync.Mutex
	used map[string]bool // signature (b64) -> consumed
}

// NewMinter returns a Minter signing with key.
func NewMinter(key []byte) *Minter {
	return &Minter{key: key, now: time.Now, used: map[string]bool{}}
}

// Mint issues a ticket bound to {streamID, caller, tenant}, valid for ttl.
func (m *Minter) Mint(streamID, caller, tenant string, ttl time.Duration) ([]byte, error) {
	c := claims{StreamID: streamID, CallerPlugin: caller, Tenant: tenant, ExpiresUnixMs: m.now().Add(ttl).UnixMilli()}
	return json.Marshal(wireTicket{Claims: c, Sig: m.sign(c)})
}

// Validate checks signature, expiry, and binding, then consumes the ticket
// (single-use): a second Validate of the same ticket returns ErrReplay.
func (m *Minter) Validate(ticket []byte, streamID, caller, tenant string) error {
	var wt wireTicket
	if json.Unmarshal(ticket, &wt) != nil {
		return ErrMalformed
	}
	if !hmac.Equal(wt.Sig, m.sign(wt.Claims)) {
		return ErrTampered
	}
	if m.now().UnixMilli() > wt.Claims.ExpiresUnixMs {
		return ErrExpired
	}
	if wt.Claims.StreamID != streamID || wt.Claims.CallerPlugin != caller || wt.Claims.Tenant != tenant {
		return ErrNotBound
	}
	id := base64.StdEncoding.EncodeToString(wt.Sig)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.used[id] {
		return ErrReplay
	}
	m.used[id] = true
	return nil
}

// sign is a deterministic HMAC over the claims (NUL-separated scalars — injective,
// no map ordering to worry about).
func (m *Minter) sign(c claims) []byte {
	h := hmac.New(sha256.New, m.key)
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d", c.StreamID, c.CallerPlugin, c.Tenant, c.ExpiresUnixMs)
	return h.Sum(nil)
}
