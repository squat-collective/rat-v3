// Command probeplugin is a launch target that REPORTS its own sandbox from inside the
// container, so the podman deployment-runtime test can prove the full I9 profile was
// actually enforced by the kernel — not merely requested. It serves StateService (like
// stateplugin) and answers Get with a JSON receipt of what it observes about its own
// process: uid (run_as_non_root), effective capabilities + no_new_privs (from
// /proc/self/status), whether the root fs is writable (read_only_root_fs), and whether
// the cloud metadata endpoint is reachable (block_metadata_egress).
//
// Built static (CGO_ENABLED=0) so it runs in a FROM-scratch image. Not a production
// plugin — a spike launch target for the full-profile isolation proof (ADR-016 §4).
package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"strings"
	"time"

	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
)

// report is the in-sandbox self-observation returned in GetResponse.value.
type report struct {
	PID               int    `json:"pid"`
	UID               int    `json:"uid"`
	CapEff            string `json:"cap_eff"`            // hex; all-zero == every capability dropped
	NoNewPrivs        string `json:"no_new_privs"`       // "1" == no_new_privileges set
	RootWritable      bool   `json:"root_writable"`      // false == read-only root fs
	MetadataReachable bool   `json:"metadata_reachable"` // false == metadata egress blocked
}

// procField returns the value of a /proc/self/status field (e.g. "CapEff", "NoNewPrivs").
func procField(field string) string {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, field+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		}
	}
	return ""
}

// rootWritable reports whether the container's root filesystem accepts a write.
func rootWritable() bool {
	f, err := os.OpenFile("/rat-rw-probe", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false // EROFS under --read-only
	}
	_ = f.Close()
	_ = os.Remove("/rat-rw-probe")
	return true
}

// metadataReachable reports whether the cloud metadata endpoint is reachable.
func metadataReachable() bool {
	c, err := net.DialTimeout("tcp", "169.254.169.254:80", 800*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

type server struct {
	statev1.UnimplementedStateServiceServer
}

func (server) Get(_ context.Context, _ *statev1.GetRequest) (*statev1.GetResponse, error) {
	b, _ := json.Marshal(report{
		PID:               os.Getpid(),
		UID:               os.Getuid(),
		CapEff:            procField("CapEff"),
		NoNewPrivs:        procField("NoNewPrivs"),
		RootWritable:      rootWritable(),
		MetadataReachable: metadataReachable(),
	})
	return &statev1.GetResponse{Found: true, Value: b}, nil
}

func main() {
	addr := os.Getenv("RAT_PLUGIN_ADDR")
	if addr == "" {
		log.Fatal("probeplugin: RAT_PLUGIN_ADDR not set")
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("probeplugin: listen %s: %v", addr, err)
	}
	s := grpc.NewServer()
	statev1.RegisterStateServiceServer(s, server{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("probeplugin: serve: %v", err)
	}
}
