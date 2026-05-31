// Package main — rat-state-inmemory-go: a reference `kind: state-backend` plugin.
//
// Sub-phase-0d reference (ADR-003). A state-backend backs the core's State Gateway
// (a tier-0 plugin — the core can't start without one). This reference implements
// Get/Put/List/Watch in memory, including the two contract obligations the state
// axis is most pointed about:
//   - the KEY GRAMMAR (freeze-blocker #3 / SEC-2) — keys/prefixes must be rejected
//     so namespace prefixing is a real boundary (see grammar.go, this file);
//   - single-key compare-and-set via the PutOutcome enum (COMMITTED/CONFLICT) and
//     an ordered Watch (the lease primitive — reviews/06 C-4).
//
// SCOPE (round 1): in-memory, so CAS is trivially linearizable and never returns
// UNKNOWN (no timeouts/partitions). The SEMANTIC test — does CAS actually serialize
// under contention? — is what a round-2 real backend (sqlite) is for. The C3
// namespace-prefixing is the core State Gateway's job, not the backend's; this
// reference stores plugin-relative keys as given and validates the grammar
// defensively (the conformant gateway is the primary enforcement point).
package main

import (
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxKeyBytes = 512

// validateKey enforces the KEY GRAMMAR (state.proto header). allowEmpty is true for
// prefixes (List/Watch), false for keys (Get/Put). Rejection → INVALID_ARGUMENT.
func validateKey(s string, allowEmpty bool) error {
	if s == "" {
		if allowEmpty {
			return nil
		}
		return status.Error(codes.InvalidArgument, "key is required")
	}
	if len(s) > maxKeyBytes {
		return status.Errorf(codes.InvalidArgument, "key exceeds %d bytes", maxKeyBytes)
	}
	if !utf8.ValidString(s) {
		return status.Error(codes.InvalidArgument, "key is not valid UTF-8")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 { // NUL + any ASCII control char
			return status.Error(codes.InvalidArgument, "key contains a control character")
		}
	}
	if strings.Contains(s, "../") || strings.Contains(s, "..\\") {
		return status.Error(codes.InvalidArgument, "key contains a path-traversal sequence")
	}
	for _, comp := range strings.Split(s, "/") {
		if comp == "." || comp == ".." {
			return status.Error(codes.InvalidArgument, "key contains a '.' or '..' path component")
		}
	}
	return nil
}
