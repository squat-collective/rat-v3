// Command formatplugin is the promoted composition-test format fake as a
// standalone FormatService binary, used to exercise the local-process
// deployment-runtime (ADR-016): it binds RAT_PLUGIN_ADDR and serves formatsvc,
// whose write snapshots are tagged with this process's PID so a caller can prove
// the work ran in a distinct OS process. Not a production plugin — a spike launch
// target for the composition-through-launched test.
package main

import (
	"log"
	"net"
	"os"

	"github.com/rat-dev/rat/core/testplugins/formatsvc"
	formatv1 "github.com/rat-dev/rat/gen/rat/format/v1"
	"google.golang.org/grpc"
)

func main() {
	addr := os.Getenv("RAT_PLUGIN_ADDR")
	if addr == "" {
		log.Fatal("formatplugin: RAT_PLUGIN_ADDR not set")
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("formatplugin: listen %s: %v", addr, err)
	}
	s := grpc.NewServer()
	formatv1.RegisterFormatServiceServer(s, formatsvc.New())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("formatplugin: serve: %v", err)
	}
}
