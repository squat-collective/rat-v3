// server.go — the RuntimeService gRPC implementation (server-streaming).
//
// Execute (rat://runtime/v1/execute) streams ExecuteResponse messages: `steps`
// interim ExecuteProgress updates, then one terminal ExecuteCompleted. Fraction is
// present as (i+1)/steps unless the work is indeterminate (then absent, exercising
// the proto3 optional double). An empty work_spec is INVALID_ARGUMENT (returned as
// the stream's terminal error before any message).
//
// RequestContext is NOT a field (ADR-007); this reference needs no identity and
// ignores the rat-callmeta-bin envelope.
package main

import (
	"fmt"

	commonv1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/common/v1"
	runtimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/runtime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeServer struct {
	runtimev1.UnimplementedRuntimeServiceServer
}

func newServer() *runtimeServer { return &runtimeServer{} }

func progressEvent(p *runtimev1.ExecuteProgress) *runtimev1.ExecuteResponse {
	return &runtimev1.ExecuteResponse{Event: &runtimev1.ExecuteResponse_Progress{Progress: p}}
}

func completedEvent(c *runtimev1.ExecuteCompleted) *runtimev1.ExecuteResponse {
	return &runtimev1.ExecuteResponse{Event: &runtimev1.ExecuteResponse_Completed{Completed: c}}
}

func (s *runtimeServer) Execute(req *runtimev1.ExecuteRequest, stream runtimev1.RuntimeService_ExecuteServer) error {
	spec, err := parseWorkSpec(req.GetWorkSpec())
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if spec.Fail != "" {
		return stream.Send(completedEvent(&runtimev1.ExecuteCompleted{Success: false, Error: spec.Fail}))
	}

	for i := 0; i < spec.Steps; i++ {
		prog := &runtimev1.ExecuteProgress{Message: fmt.Sprintf("step %d/%d", i+1, spec.Steps)}
		if !spec.Indeterminate {
			f := float64(i+1) / float64(spec.Steps)
			prog.Fraction = &f // present == determinate progress
		}
		if err := stream.Send(progressEvent(prog)); err != nil {
			return err
		}
	}

	rows := spec.Rows
	return stream.Send(completedEvent(&runtimev1.ExecuteCompleted{
		Success: true,
		Result:  &commonv1.WriteResult{RowsAffected: &rows},
	}))
}
