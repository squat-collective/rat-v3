// Command stateplugin is a minimal standalone StateService plugin used to exercise
// the local-process deployment-runtime (ADR-016): it binds RAT_PLUGIN_ADDR and
// answers Get with a value tagged by its own PID, so a caller can prove the work
// ran in a distinct OS process. Not a production plugin — a spike launch target.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	statev1 "github.com/le-squat/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
)

type server struct {
	statev1.UnimplementedStateServiceServer
}

func (server) Get(_ context.Context, req *statev1.GetRequest) (*statev1.GetResponse, error) {
	return &statev1.GetResponse{Found: true, Value: []byte(fmt.Sprintf("pid=%d key=%s", os.Getpid(), req.GetKey()))}, nil
}

func main() {
	addr := os.Getenv("RAT_PLUGIN_ADDR")
	if addr == "" {
		log.Fatal("stateplugin: RAT_PLUGIN_ADDR not set")
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("stateplugin: listen %s: %v", addr, err)
	}
	s := grpc.NewServer()
	statev1.RegisterStateServiceServer(s, server{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("stateplugin: serve: %v", err)
	}
}
