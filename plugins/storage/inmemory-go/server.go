// server.go — the StorageService gRPC implementation.
//
// Implements VendCredentials (rat://storage/v1/vend-credentials): validate the
// request, read the caller's tenant FROM THE rat-callmeta-bin METADATA HEADER
// (ADR-007 — this is the first reference that actually consumes identity from the
// envelope, because tenant-scoping is storage's whole job), and vend a short-TTL
// scope receipt bound to (tenant, prefix, mode).
//
// SECURITY note (reviews/04, C7): the tenant comes ONLY from the core-stamped
// metadata, never from a request field — there is no way for the caller to ask for
// a different tenant's creds. That structural property is what the conformance
// harness asserts (the vended scope.tenant == the metadata tenant).
package main

import (
	"context"

	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	storagev1 "github.com/squat-collective/rat-v3/gen/rat/storage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const callMetaHeader = "rat-callmeta-bin"

type storageServer struct {
	storagev1.UnimplementedStorageServiceServer
	now func() int64 // injectable clock (ms); real time by default
}

func newServer() *storageServer { return &storageServer{now: nowMs} }

// tenantFromContext reads the caller's tenant out of the rat-callmeta-bin metadata
// envelope (ADR-007). Empty (== the single-tenant/solo default) if absent.
func tenantFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get(callMetaHeader)
	if len(vals) == 0 {
		return ""
	}
	var rc commonv1.RequestContext
	if proto.Unmarshal([]byte(vals[0]), &rc) != nil {
		return ""
	}
	return rc.GetIdentity().GetTenant()
}

// VendCredentials issues short-TTL creds scoped to (tenant, prefix, mode).
func (s *storageServer) VendCredentials(ctx context.Context, req *storagev1.VendCredentialsRequest) (*storagev1.VendCredentialsResponse, error) {
	if req.GetPrefix() == "" {
		return nil, status.Error(codes.InvalidArgument, "prefix is required")
	}
	if req.GetMode() == storagev1.AccessMode_ACCESS_MODE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "mode must be specified")
	}
	expires := s.now() + credentialTTLms
	receipt := scopeReceipt{
		Tenant:        tenantFromContext(ctx), // from metadata, never a request field
		Prefix:        req.GetPrefix(),
		Mode:          modeString(req.GetMode()),
		ExpiresUnixMs: expires,
	}
	return &storagev1.VendCredentialsResponse{
		Credentials:   receipt.encode(),
		ExpiresUnixMs: expires,
	}, nil
}
