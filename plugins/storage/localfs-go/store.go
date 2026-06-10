// Package main — rat-storage-localfs-go: the ROUND-2 `storage` reference (ADR-003).
//
// A technologically-divergent backend, not another in-memory scope-receipt echo:
// it vends credentials scoped to a REAL local filesystem path under a per-tenant
// root, and it ENFORCES containment — a prefix that would escape the tenant root is
// denied. That path-containment / tenant-isolation security property is the storage
// analog of what sqlite gave `state` (durability + linearizable CAS): something a
// real backend has to EARN and the in-memory stand-in can only fake.
//
// It still passes the SAME shared golden vectors (the scope binding: tenant + the
// logical prefix + mode + short TTL); the real-filesystem behavior is exercised by
// round-2-specific tests (harness_test.go).
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	storagev1 "github.com/squat-collective/rat-v3/gen/rat/storage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const credentialTTLms int64 = 900_000 // 15 minutes — the documented short-TTL

// scopeReceipt is the conformance credential blob. Beyond the provider-neutral
// fields the in-memory ref carries, local-fs adds `resolved_path` — the REAL local
// path the creds grant access to. The shared vectors ignore it; the round-2 tests
// assert it (containment + the dir was created on disk).
type scopeReceipt struct {
	Tenant        string `json:"tenant"`
	Prefix        string `json:"prefix"`
	Mode          string `json:"mode"`
	ExpiresUnixMs int64  `json:"expires_unix_ms"`
	ResolvedPath  string `json:"resolved_path"`
}

func (r scopeReceipt) encode() []byte {
	b, _ := json.Marshal(r)
	return b
}

func modeString(m storagev1.AccessMode) string {
	switch m {
	case storagev1.AccessMode_ACCESS_MODE_READ:
		return "READ"
	case storagev1.AccessMode_ACCESS_MODE_WRITE:
		return "WRITE"
	case storagev1.AccessMode_ACCESS_MODE_READ_WRITE:
		return "READ_WRITE"
	default:
		return "UNSPECIFIED"
	}
}

// store is a real local-filesystem storage backend. Tenant roots are <root>/<tenant>.
type store struct{ root string }

func newStore(root string) *store { return &store{root: root} }

// vend resolves the logical prefix to a real path under the tenant root, ENFORCES
// containment (an escaping prefix → PERMISSION_DENIED, the cross-tenant boundary
// storage.proto emphasizes), creates the directory, and returns the scope receipt.
// `tenant` is supplied by the server (read from the rat-callmeta-bin metadata).
func (s *store) vend(tenant, prefix, mode string, expires int64) (scopeReceipt, error) {
	tenantRoot := filepath.Join(s.root, tenant)
	// filepath.Join cleans "..", so a prefix that climbs out lands OUTSIDE tenantRoot.
	resolved := filepath.Join(tenantRoot, prefix)
	rel, err := filepath.Rel(tenantRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return scopeReceipt{}, status.Errorf(codes.PermissionDenied, "prefix %q escapes the tenant root", prefix)
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		return scopeReceipt{}, status.Errorf(codes.Internal, "mkdir %s: %v", resolved, err)
	}
	return scopeReceipt{Tenant: tenant, Prefix: prefix, Mode: mode, ExpiresUnixMs: expires, ResolvedPath: resolved}, nil
}
