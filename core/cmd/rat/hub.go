package main

// rat hub — workspace federation (ADR-033): a gateway-of-gateways. One endpoint fans out to many
// workspace daemons. The hub implements CapabilityInvokeService as a GENERIC byte-relay (like the
// per-plane gateway, ADR-005): it reads the `rat-workspace` selector from transport metadata,
// resolves it to a running workspace via the instance registry (the same source as `rat ls`), and
// forwards the opaque InvokeRequest there — PRESERVING the rat-callmeta-bin envelope (ADR-007) so
// the workspace does its own C5/C7/C8 against the original caller. The hub interprets neither the
// capability nor the payload, and holds no plane state.
//
// First cut: unary Invoke + local-registry discovery. Streams/ControlService forwarding, identity-
// at-hub + TLS, and the NATS-leaf cross-machine transport are ADR-033 Q01-Q03 (refinements).

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strings"

	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// workspaceHeader is the transport-metadata key a client sets (via `rat call --workspace`) to pick
// a workspace. The hub routes on it; a bare daemon harmlessly ignores it (ADR-033 §4).
const workspaceHeader = "rat-workspace"

// hubServer relays capability invocations to the workspace named in the rat-workspace header.
type hubServer struct {
	corev1.UnimplementedCapabilityInvokeServiceServer
}

func (h *hubServer) Invoke(ctx context.Context, req *corev1.InvokeRequest) (*corev1.InvokeResponse, error) {
	ws := firstMeta(ctx, workspaceHeader)
	if ws == "" {
		return nil, status.Error(codes.InvalidArgument,
			"no "+workspaceHeader+" (use `rat call --hub <addr> --workspace <name> …`)")
	}
	addr, ok := workspaceAddr(ws)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "unknown workspace %q (running: %s)",
			ws, strings.Join(runningWorkspaces(), ", "))
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "dial workspace %q at %s: %v", ws, addr, err)
	}
	defer conn.Close()
	// Forward the caller's metadata verbatim (rat-callmeta-bin identity + trace) so the workspace
	// authorizes the ORIGINAL caller, not the hub. The hub adds no identity of its own (Q03).
	out := ctx
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		out = metadata.NewOutgoingContext(ctx, md)
	}
	return corev1.NewCapabilityInvokeServiceClient(conn).Invoke(out, req)
}

func firstMeta(ctx context.Context, key string) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(key); len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// workspaceAddr resolves a workspace name -> its daemon addr from the LIVE instance registry, so a
// workspace started after the hub is picked up automatically (discovery is per-call, not cached).
func workspaceAddr(name string) (string, bool) {
	for _, e := range pruneRegistry(loadRegistry()) {
		if e.Name == name {
			return e.Addr, true
		}
	}
	return "", false
}

func runningWorkspaces() []string {
	var ns []string
	for _, e := range pruneRegistry(loadRegistry()) {
		ns = append(ns, e.Name)
	}
	return ns
}

// runHub serves the federation front door: `rat hub [--addr host:port]`.
func runHub(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat hub", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7700", "hub listen address (TCP)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("hub listen on %s: %w", *addr, err)
	}
	srv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(srv, &hubServer{})

	ws := runningWorkspaces()
	fmt.Fprintf(out, "rat hub — federating %d workspace(s): %s\n", len(ws), orNone(ws))
	fmt.Fprintf(out, "listening on %s\n", lis.Addr())
	fmt.Fprintf(out, "  rat call --hub %s --workspace <name> rat://… --as <caller>\n", lis.Addr())
	log.Printf("up on %s (discovery: %s)", lis.Addr(), registryFile())
	return srv.Serve(lis)
}

func orNone(ws []string) string {
	if len(ws) == 0 {
		return "(none yet — discovered live per call)"
	}
	return strings.Join(ws, ", ")
}
