// main.go — entrypoint: serve StorageService backed by a local-fs root. Root from
// $RAT_STORAGE_ROOT (default ./storage-root); address from $RAT_PLUGIN_ADDR
// (default :0 → an OS-assigned port, printed on startup).
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	storagev1 "github.com/le-squat/rat/gen/rat/storage/v1"
	"google.golang.org/grpc"
)

func nowMs() int64 { return time.Now().UnixMilli() }

func main() {
	root := os.Getenv("RAT_STORAGE_ROOT")
	if root == "" {
		root = "storage-root"
	}
	addr := os.Getenv("RAT_PLUGIN_ADDR")
	if addr == "" {
		addr = ":0"
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	storagev1.RegisterStorageServiceServer(srv, newServer(newStore(root)))
	fmt.Printf("rat-storage-localfs-go listening on %s (root=%s)\n", lis.Addr().String(), root)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
