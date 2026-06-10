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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	// Nonce makes every minted ticket UNIQUE even when {stream,caller,tenant,expiry} are identical
	// (e.g. two tickets for one stream minted in the same millisecond). The single-use id is derived
	// from the signature, which covers the nonce, so two distinct mints can never collide on the
	// single-use set — without it, same-millisecond re-mints would falsely trip replay detection.
	Nonce string `json:"n"`
}

type wireTicket struct {
	Claims claims `json:"claims"`
	Sig    []byte `json:"sig"`
}

// SingleUseStore records consumed ticket ids so a ticket is redeemable at most once. The
// default is in-memory + per-process; a SHARED implementation (NewCASStore over a backend with
// atomic create-if-absent) closes the replay window across producer RESTART and REPLICAS — gap
// #7: a per-process set lets a restarted/replicated producer reopen replay.
type SingleUseStore interface {
	// Consume atomically marks id used, returning firstUse=false if it was ALREADY consumed (a
	// replay). A non-nil err means the store could NOT confirm — the caller fails CLOSED
	// (rejects the ticket), since an unconfirmable single-use check can't guarantee no replay.
	Consume(id string) (firstUse bool, err error)
}

// Minter mints + validates tickets under one secret key, enforcing single-use via its store.
type Minter struct {
	key   []byte
	now   func() time.Time // injectable clock (tests)
	store SingleUseStore
}

// NewMinter returns a Minter signing with key, using an in-memory (per-process) single-use
// store. Sufficient for a single producer; use NewMinterWithStore for restart/replica safety.
func NewMinter(key []byte) *Minter {
	return NewMinterWithStore(key, NewMemStore())
}

// NewMinterWithStore returns a Minter backed by a caller-supplied single-use store — e.g. a
// shared CAS store so replay can't be reopened by a restart or a second replica.
func NewMinterWithStore(key []byte, store SingleUseStore) *Minter {
	return &Minter{key: key, now: time.Now, store: store}
}

// Mint issues a UNIQUE ticket bound to {streamID, caller, tenant}, valid for ttl. Each call gets a
// fresh random nonce, so two tickets minted with identical binding + expiry (same millisecond) are
// still distinct single-use credentials.
func (m *Minter) Mint(streamID, caller, tenant string, ttl time.Duration) ([]byte, error) {
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	c := claims{StreamID: streamID, CallerPlugin: caller, Tenant: tenant, ExpiresUnixMs: m.now().Add(ttl).UnixMilli(), Nonce: nonce}
	return json.Marshal(wireTicket{Claims: c, Sig: m.sign(c)})
}

// newNonce returns a 128-bit random hex nonce.
func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("arrowticket: nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
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
	firstUse, err := m.store.Consume(id)
	if err != nil {
		return fmt.Errorf("arrowticket: single-use store: %w", err) // fail closed
	}
	if !firstUse {
		return ErrReplay
	}
	return nil
}

// sign is a deterministic HMAC over the claims (NUL-separated scalars — injective,
// no map ordering to worry about). The nonce is covered, so each mint signs uniquely.
func (m *Minter) sign(c claims) []byte {
	h := hmac.New(sha256.New, m.key)
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%s", c.StreamID, c.CallerPlugin, c.Tenant, c.ExpiresUnixMs, c.Nonce)
	return h.Sum(nil)
}

// MemStore is the default in-memory SingleUseStore (per-process). A restart or a second replica
// starts with an empty set, so it cannot detect a ticket consumed elsewhere — use a shared
// CASStore when a producer is restarted or replicated.
type MemStore struct {
	mu   sync.Mutex
	used map[string]bool
}

// NewMemStore returns an empty in-memory single-use store.
func NewMemStore() *MemStore { return &MemStore{used: map[string]bool{}} }

// Consume marks id used; firstUse=false on a second call. Never errors.
func (s *MemStore) Consume(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used[id] {
		return false, nil
	}
	s.used[id] = true
	return true, nil
}

// SingleUseCAS is the minimal primitive a SHARED single-use store needs: an ATOMIC
// create-if-absent. Note: the frozen state/v1 lacks this (ADR-043 Q01); a shared store therefore
// rides a backend that has it natively — etcd txn, Redis SETNX, or a DB unique constraint — until
// a create-if-absent amendment lands. Atomicity is the whole point: two concurrent redemptions of
// one ticket must yield exactly one created=true.
type SingleUseCAS interface {
	// PutIfAbsent atomically stores key, returning created=false if it already existed.
	PutIfAbsent(key string) (created bool, err error)
}

// CASStore is the SHARED, durable SingleUseStore: it records consumed ticket ids in a backend with
// atomic create-if-absent, so replay can't be reopened by a producer restart or a second replica
// (gap #7). keyPrefix namespaces the ticket keys within the backend.
type CASStore struct {
	cas       SingleUseCAS
	keyPrefix string
}

// NewCASStore returns a single-use store over cas, prefixing ticket keys with keyPrefix.
func NewCASStore(cas SingleUseCAS, keyPrefix string) *CASStore {
	return &CASStore{cas: cas, keyPrefix: keyPrefix}
}

// Consume create-if-absents the ticket key: created=true is the first (only) valid redemption.
func (s *CASStore) Consume(id string) (bool, error) {
	return s.cas.PutIfAbsent(s.keyPrefix + id)
}
