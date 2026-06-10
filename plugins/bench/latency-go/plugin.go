// Package main — the RAT per-RPC latency benchmark (sub-phase 0f).
//
// It quantifies the one perf number the architecture actually trades on: the
// core-mediated gateway's overhead vs a direct call (ADR-005 accepted "a latency hop
// per control call"; ADR-008 added a streaming relay). It measures the SAME plugin
// RPC two ways — direct (caller → plugin) and mediated (caller → gateway → plugin) —
// for a unary RPC (state.Get) and a server-streaming one (runtime.Execute), and
// reports p50/p99/mean + the mediation delta.
//
// The plugin RPCs are deliberately trivial (a fixed response / a few fixed frames)
// so the measurement isolates TRANSPORT + MEDIATION cost, not the plugin's work.
package main

import (
	"context"
	"fmt"

	commonv1 "github.com/le-squat/rat/gen/rat/common/v1"
	runtimev1 "github.com/le-squat/rat/gen/rat/runtime/v1"
	statev1 "github.com/le-squat/rat/gen/rat/state/v1"
)

// plugin serves both a unary (StateService.Get) and a streaming (RuntimeService.
// Execute) capability so one gateway can mediate both.
type plugin struct {
	statev1.UnimplementedStateServiceServer
	runtimev1.UnimplementedRuntimeServiceServer
}

func (p *plugin) Get(_ context.Context, _ *statev1.GetRequest) (*statev1.GetResponse, error) {
	rev := int64(1)
	return &statev1.GetResponse{Found: true, Value: []byte("benchval"), Revision: rev}, nil
}

func (p *plugin) Execute(_ *runtimev1.ExecuteRequest, stream runtimev1.RuntimeService_ExecuteServer) error {
	for i := 0; i < 3; i++ {
		f := float64(i+1) / 3
		if err := stream.Send(&runtimev1.ExecuteResponse{Event: &runtimev1.ExecuteResponse_Progress{
			Progress: &runtimev1.ExecuteProgress{Fraction: &f, Message: fmt.Sprintf("step %d/3", i+1)},
		}}); err != nil {
			return err
		}
	}
	rows := int64(42)
	return stream.Send(&runtimev1.ExecuteResponse{Event: &runtimev1.ExecuteResponse_Completed{
		Completed: &runtimev1.ExecuteCompleted{Success: true, Result: &commonv1.WriteResult{RowsAffected: &rows}},
	}})
}
