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
	identityv1 "github.com/rat-dev/rat/gen/rat/identity/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// workspaceHeader: which workspace to route to (set by `rat call --workspace`). A bare daemon
	// ignores it (ADR-033 §4). tokenHeader: the bearer credential the edge authenticates (ADR-034).
	workspaceHeader = "rat-workspace"
	tokenHeader     = "rat-token"
)

// hubServer relays capability invocations to the workspace named in the rat-workspace header. When
// identityAddr is set, it AUTHENTICATES the caller at the edge first (ADR-034 §3 / ADR-033 Q03).
type hubServer struct {
	corev1.UnimplementedCapabilityInvokeServiceServer
	identityAddr string // identity provider gRPC addr; "" = no edge auth (localhost-trust only)
}

// authenticate gates a call at the edge: the caller must present a `rat-token` the identity plugin
// validates. This is the seam that closes the trust-asserted `--as` gap — the security model (ADR-034)
// delegates "who?" to the identity plugin; the hub enforces it before any workspace is reached.
func (h *hubServer) authenticate(ctx context.Context) error {
	tok := firstMeta(ctx, tokenHeader)
	if tok == "" {
		return status.Error(codes.Unauthenticated, "missing "+tokenHeader+" — this hub requires authentication (`rat call --token <t>`)")
	}
	conn, err := grpc.NewClient(h.identityAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return status.Errorf(codes.Unavailable, "identity provider %s: %v", h.identityAddr, err)
	}
	defer conn.Close()
	resp, err := identityv1.NewIdentityServiceClient(conn).Authenticate(ctx,
		&identityv1.AuthenticateRequest{Credential: []byte(tok)})
	if err != nil {
		return status.Errorf(codes.Unavailable, "authenticate: %v", err)
	}
	if !resp.GetAuthenticated() {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	log.Printf("authenticated subject=%q tenant=%q", resp.GetSubject(), resp.GetTenant())
	return nil
}

func (h *hubServer) Invoke(ctx context.Context, req *corev1.InvokeRequest) (*corev1.InvokeResponse, error) {
	if h.identityAddr != "" {
		if err := h.authenticate(ctx); err != nil {
			return nil, err
		}
	}
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

// runHub serves the federation front door: `rat hub [flags]`. Enforces the secure-by-default
// binding posture (ADR-034 §4): a PUBLIC (non-loopback) bind requires TLS + an identity provider,
// or an explicit --insecure override. A localhost bind stays open (filesystem/loopback trust).
func runHub(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat hub", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7700", "hub listen address (TCP)")
	tlsCert := fs.String("tls-cert", "", "TLS certificate (PEM) — required for a public bind")
	tlsKey := fs.String("tls-key", "", "TLS private key (PEM) — required for a public bind")
	identityAddr := fs.String("identity", "", "identity provider gRPC addr; when set, callers must present a valid --token")
	insecurePublic := fs.Bool("insecure", false, "allow a public bind without TLS/identity (DEV ONLY — loud warning)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// --- secure-by-default binding guardrail (ADR-034 §4) ---
	public := isPublicAddr(*addr)
	tlsOn := *tlsCert != "" && *tlsKey != ""
	if public && !*insecurePublic {
		if !tlsOn {
			return fmt.Errorf("refusing public bind %s without TLS — pass --tls-cert/--tls-key (and --identity), "+
				"or --insecure to override (DEV ONLY). See ADR-034 (secure-by-default).", *addr)
		}
		if *identityAddr == "" {
			return fmt.Errorf("refusing public bind %s without an identity provider — pass --identity <addr>, "+
				"or --insecure to override (DEV ONLY). See ADR-034.", *addr)
		}
	}

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("hub listen on %s: %w", *addr, err)
	}
	var opts []grpc.ServerOption
	scheme := "plaintext"
	if tlsOn {
		creds, err := credentials.NewServerTLSFromFile(*tlsCert, *tlsKey)
		if err != nil {
			return fmt.Errorf("load TLS cert/key: %w", err)
		}
		opts = append(opts, grpc.Creds(creds))
		scheme = "TLS"
	}
	srv := grpc.NewServer(opts...)
	corev1.RegisterCapabilityInvokeServiceServer(srv, &hubServer{identityAddr: *identityAddr})

	ws := runningWorkspaces()
	fmt.Fprintf(out, "rat hub — federating %d workspace(s): %s\n", len(ws), orNone(ws))
	fmt.Fprintf(out, "listening on %s  ·  transport=%s  ·  bind=%s  ·  auth=%s\n",
		lis.Addr(), scheme, posture(public), authMode(*identityAddr))
	if public && *insecurePublic && (!tlsOn || *identityAddr == "") {
		fmt.Fprintln(out, "⚠️  INSECURE: public bind without full TLS+identity — DEV ONLY, never in production")
	}
	fmt.Fprintf(out, "  rat call --hub %s --workspace <name> rat://… --as <caller>%s\n",
		lis.Addr(), hintToken(*identityAddr))
	log.Printf("up on %s (transport=%s, discovery=%s)", lis.Addr(), scheme, registryFile())
	return srv.Serve(lis)
}

// isPublicAddr reports whether a listen addr is reachable off-host (non-loopback) — the trigger for
// the secure-by-default guardrail. A wildcard (0.0.0.0/::) or any non-loopback IP/host is "public".
func isPublicAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	switch host {
	case "", "0.0.0.0", "::":
		return true // wildcard bind = reachable on every interface
	case "localhost":
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	return true // a non-localhost hostname → treat as public
}

func posture(public bool) string {
	if public {
		return "public"
	}
	return "local"
}
func authMode(identityAddr string) string {
	if identityAddr != "" {
		return "required (identity " + identityAddr + ")"
	}
	return "none (localhost-trust)"
}
func hintToken(identityAddr string) string {
	if identityAddr != "" {
		return " --token <t>"
	}
	return ""
}

func orNone(ws []string) string {
	if len(ws) == 0 {
		return "(none yet — discovered live per call)"
	}
	return strings.Join(ws, ", ")
}
