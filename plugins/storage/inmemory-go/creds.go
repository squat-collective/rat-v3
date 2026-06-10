// Package main — rat-storage-inmemory-go: a reference `kind: storage` plugin.
//
// Sub-phase-0d reference (ADR-003). Storage owns byte storage + credential
// vending; the control plane never sees bytes — it asks storage to vend
// short-TTL, prefix-scoped, TENANT-scoped credentials, and the engine/format then
// talk to object storage directly with them.
//
// This reference vends a CONFORMANCE "scope receipt" rather than a real STS token:
// a production storage plugin (S3/GCS/local-fs) returns provider-specific opaque
// creds, but the reference encodes the granted scope as JSON so the harness can
// assert the C7 security obligation — the creds are bound to the caller's tenant +
// the requested prefix + mode. The point under test is the vend-credentials wire
// contract + the tenancy-scoping obligation, not real STS minting.
package main

import (
	"encoding/json"

	storagev1 "github.com/squat-collective/rat-v3/gen/rat/storage/v1"
)

// credentialTTLms is the documented short-TTL these creds carry. A real plugin
// would mint provider creds with this expiry; the reference just stamps it.
const credentialTTLms int64 = 900_000 // 15 minutes

// scopeReceipt is the CONFORMANCE stand-in for an opaque credential blob. Encoding
// the granted scope is what makes the C7 obligation observable to the harness:
// "the vended creds are scoped to (tenant, prefix, mode)". A real STS blob would
// be opaque and this assertion would instead be made by attempting access.
type scopeReceipt struct {
	Tenant        string `json:"tenant"`
	Prefix        string `json:"prefix"`
	Mode          string `json:"mode"`
	ExpiresUnixMs int64  `json:"expires_unix_ms"`
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
