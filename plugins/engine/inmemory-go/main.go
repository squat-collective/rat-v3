// main.go — entrypoint: serve EngineService over gRPC.
//
// A real `kind: engine` plugin is a gRPC server the core (or a test harness)
// dials. This wires the in-memory implementation onto a listener. Address comes
// from $RAT_PLUGIN_ADDR (default :0 → an OS-assigned port, printed on startup so a
// harness can read it); a real deployment-runtime would inject the address.
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	enginev1 "github.com/rat-dev/rat/gen/rat/engine/v1"
	"google.golang.org/grpc"
)

func snapshotID(n int64) *string { s := "snap-" + strconv.FormatInt(n, 10); return &s }

func main() {
	addr := os.Getenv("RAT_PLUGIN_ADDR")
	if addr == "" {
		addr = ":0"
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(srv, newServer())
	fmt.Printf("rat-engine-inmemory-go listening on %s\n", lis.Addr().String())
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
