// Command catalogplugin is the promoted composition-test catalog fake as a
// standalone CatalogService binary, used to exercise the local-process
// deployment-runtime (ADR-016): it binds RAT_PLUGIN_ADDR and serves catalogsvc,
// whose responses are tagged with this process's PID so a caller can prove the
// work ran in a distinct OS process. Not a production plugin — a spike launch
// target for the composition-through-launched test.
package main

import (
	"log"
	"net"
	"os"

	"github.com/le-squat/rat/core/testplugins/catalogsvc"
	catalogv1 "github.com/le-squat/rat/gen/rat/catalog/v1"
	"google.golang.org/grpc"
)

func main() {
	addr := os.Getenv("RAT_PLUGIN_ADDR")
	if addr == "" {
		log.Fatal("catalogplugin: RAT_PLUGIN_ADDR not set")
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("catalogplugin: listen %s: %v", addr, err)
	}
	s := grpc.NewServer()
	catalogv1.RegisterCatalogServiceServer(s, catalogsvc.New())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("catalogplugin: serve: %v", err)
	}
}
