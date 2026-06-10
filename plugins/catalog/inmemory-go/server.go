// server.go — the CatalogService gRPC implementation.
//
// GetTable (rat://catalog/v1/get-table) resolves an identifier (+ optional branch)
// to a TableRef; CreateBranch (…/create-branch) opens an isolated branch;
// MergeBranch (…/merge-branch) merges under the optimistic-concurrency +
// idempotency contract. RequestContext is NOT a field (ADR-007); this reference
// ignores identity.
package main

import (
	"context"

	commonv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/common/v1"
	catalogv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/catalog/v1"
)

type catalogServer struct {
	catalogv1.UnimplementedCatalogServiceServer
	cat *catalog
}

func newServer() *catalogServer { return &catalogServer{cat: newCatalog()} }

func (s *catalogServer) GetTable(_ context.Context, req *catalogv1.GetTableRequest) (*catalogv1.GetTableResponse, error) {
	branch, uri, err := s.cat.getTable(req.GetIdentifier(), req.GetBranch())
	if err != nil {
		return nil, err
	}
	return &catalogv1.GetTableResponse{Table: &commonv1.TableRef{
		Identifier: req.GetIdentifier(),
		Uri:        uri,
		Branch:     branch,
	}}, nil
}

func (s *catalogServer) CreateBranch(_ context.Context, req *catalogv1.CreateBranchRequest) (*catalogv1.CreateBranchResponse, error) {
	if err := s.cat.createBranch(req.GetBranch(), req.GetFromBranch()); err != nil {
		return nil, err
	}
	return &catalogv1.CreateBranchResponse{Branch: req.GetBranch()}, nil
}

func (s *catalogServer) MergeBranch(_ context.Context, req *catalogv1.MergeBranchRequest) (*catalogv1.MergeBranchResponse, error) {
	snap, already, err := s.cat.mergeBranch(req.GetBranch(), req.GetIntoBranch(), req.GetExpectedIntoSnapshot(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &catalogv1.MergeBranchResponse{SnapshotId: snap, AlreadyApplied: already}, nil
}

func (s *catalogServer) RegisterTable(_ context.Context, req *catalogv1.RegisterTableRequest) (*catalogv1.RegisterTableResponse, error) {
	branch, uri, err := s.cat.registerTable(req.GetIdentifier(), req.GetUri(), req.GetBranch())
	if err != nil {
		return nil, err
	}
	return &catalogv1.RegisterTableResponse{Table: &commonv1.TableRef{
		Identifier: req.GetIdentifier(),
		Uri:        uri,
		Branch:     branch,
	}}, nil
}

func (s *catalogServer) CommitTable(_ context.Context, req *catalogv1.CommitTableRequest) (*catalogv1.CommitTableResponse, error) {
	snap, already, err := s.cat.commitTable(req.GetIdentifier(), req.GetBranch(), req.GetSnapshotId(), req.GetExpectedSnapshot(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &catalogv1.CommitTableResponse{SnapshotId: snap, AlreadyApplied: already}, nil
}
