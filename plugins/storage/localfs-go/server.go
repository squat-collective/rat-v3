// server.go — the StorageService gRPC implementation over the local-fs store.
//
// Same surface + validation as the in-memory reference (empty prefix / unspecified
// mode → INVALID_ARGUMENT; tenant read from the rat-callmeta-bin metadata header,
// ADR-007, never a request field); the store does the real filesystem resolution +
// containment.
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
	store *store
	now   func() int64
}

func newServer(st *store) *storageServer { return &storageServer{store: st, now: nowMs} }

// tenantFromContext reads the caller's tenant out of the rat-callmeta-bin metadata
// envelope (ADR-007). Empty (== solo default) if absent.
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

func (s *storageServer) VendCredentials(ctx context.Context, req *storagev1.VendCredentialsRequest) (*storagev1.VendCredentialsResponse, error) {
	if req.GetPrefix() == "" {
		return nil, status.Error(codes.InvalidArgument, "prefix is required")
	}
	if req.GetMode() == storagev1.AccessMode_ACCESS_MODE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "mode must be specified")
	}
	expires := s.now() + credentialTTLms
	receipt, err := s.store.vend(tenantFromContext(ctx), req.GetPrefix(), modeString(req.GetMode()), expires)
	if err != nil {
		return nil, err // PERMISSION_DENIED on a containment violation
	}
	return &storagev1.VendCredentialsResponse{
		Credentials:   receipt.encode(),
		ExpiresUnixMs: expires,
	}, nil
}
