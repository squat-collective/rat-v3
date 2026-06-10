// main.go — entrypoint: serve StorageService over gRPC. Address from
// $RAT_PLUGIN_ADDR (default :0 → an OS-assigned port, printed on startup).
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	storagev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/storage/v1"
	"google.golang.org/grpc"
)

func nowMs() int64 { return time.Now().UnixMilli() }

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
	storagev1.RegisterStorageServiceServer(srv, newServer())
	fmt.Printf("rat-storage-inmemory-go listening on %s\n", lis.Addr().String())
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
