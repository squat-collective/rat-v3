// main.go — entrypoint: serve RuntimeService over gRPC. Address from
// $RAT_PLUGIN_ADDR (default :0 → an OS-assigned port, printed on startup).
package main

import (
	"fmt"
	"log"
	"net"
	"os"

	runtimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/runtime/v1"
	"google.golang.org/grpc"
)

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
	runtimev1.RegisterRuntimeServiceServer(srv, newServer())
	fmt.Printf("rat-runtime-inmemory-go listening on %s\n", lis.Addr().String())
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
