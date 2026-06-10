package main

// rat hub — workspace federation (ADR-033): a gateway-of-gateways. One endpoint fans out to many
// workspace daemons. The hub is a TRANSPARENT gRPC PROXY (ADR-033 Q02): it reads the `rat-workspace`
// selector from transport metadata, resolves it to a running workspace via the instance registry (the
// same source as `rat ls`), and relays the RPC — PRESERVING the rat-callmeta-bin envelope (ADR-007) so
// the workspace does its own C5/C7/C8 against the original caller. It interprets neither method nor
// payload, and holds no plane state.
//
// Because it proxies via an UnknownServiceHandler + a passthrough codec (frames forwarded verbatim),
// it federates EVERY method — unary Invoke, InvokeServerStream (state.Watch / streaming engines),
// InvokeBidiStream, AND ControlService.* (remote register/deregister/status) — with no per-method
// code, so a new service crosses the hub with zero hub changes. Connections are POOLED (one long-lived
// conn per workspace + the identity provider) instead of dialed per call (gap #5). The NATS-leaf
// cross-machine transport stays ADR-033 Q01.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"

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

// connPool caches one long-lived *grpc.ClientConn per address (workspaces + the identity provider),
// so the hub stops dialing+closing a fresh connection on every call (gap #5). grpc.NewClient conns are
// safe for concurrent use and manage their own (re)connection.
type connPool struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

func newConnPool() *connPool { return &connPool{conns: map[string]*grpc.ClientConn{}} }

func (p *connPool) get(addr string) (*grpc.ClientConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.conns[addr]; c != nil {
		return c, nil
	}
	c, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	p.conns[addr] = c
	return c, nil
}

func (p *connPool) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = map[string]*grpc.ClientConn{}
}

// proxyCodec relays message bytes verbatim. Named "proto" so content-subtype negotiation is unchanged
// end-to-end (the same trick the per-plane gateway's relay uses) — the hub never deserializes a frame.
// Set per-server (ForceServerCodec) + per-call (ForceCodec), never globally, so real proto servers in
// the same process keep their codec.
type proxyCodec struct{}

func (proxyCodec) Marshal(v any) ([]byte, error)      { return v.([]byte), nil }
func (proxyCodec) Unmarshal(data []byte, v any) error { *(v.(*[]byte)) = data; return nil }
func (proxyCodec) Name() string                       { return "proto" }

// hubServer relays every RPC to the workspace named in rat-workspace. When identityAddr is set it
// AUTHENTICATES the caller at the edge first (ADR-034 §3 / ADR-033 Q03). resolve maps a workspace name
// to its daemon addr (injectable for tests; the daemon wires it to the instance registry).
type hubServer struct {
	identityAddr string
	pool         *connPool
	resolve      func(workspace string) (addr string, ok bool)
}

// authenticate gates a call at the edge: the caller must present a `rat-token` the identity plugin
// validates. Uses the pooled identity conn (no per-call dial).
func (h *hubServer) authenticate(ctx context.Context) error {
	tok := firstMeta(ctx, tokenHeader)
	if tok == "" {
		return status.Error(codes.Unauthenticated, "missing "+tokenHeader+" — this hub requires authentication (`rat call --token <t>`)")
	}
	conn, err := h.pool.get(h.identityAddr)
	if err != nil {
		return status.Errorf(codes.Unavailable, "identity provider %s: %v", h.identityAddr, err)
	}
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

// proxyStream is the generic relay wired as the server's UnknownServiceHandler: EVERY inbound RPC —
// Invoke, InvokeServerStream, InvokeBidiStream, ControlService.* — lands here and is forwarded to the
// selected workspace over a pooled connection, frames relayed verbatim in both directions.
func (h *hubServer) proxyStream(_ any, serverStream grpc.ServerStream) error {
	ctx := serverStream.Context()
	fullMethod, ok := grpc.MethodFromServerStream(serverStream)
	if !ok {
		return status.Error(codes.Internal, "hub: cannot read method from stream")
	}
	if h.identityAddr != "" {
		if err := h.authenticate(ctx); err != nil {
			return err
		}
	}
	ws := firstMeta(ctx, workspaceHeader)
	if ws == "" {
		return status.Error(codes.InvalidArgument, "no "+workspaceHeader+" (use `rat call --hub <addr> --workspace <name> …`)")
	}
	addr, ok := h.resolve(ws)
	if !ok {
		return status.Errorf(codes.NotFound, "unknown workspace %q (running: %s)", ws, strings.Join(runningWorkspaces(), ", "))
	}
	conn, err := h.pool.get(addr)
	if err != nil {
		return status.Errorf(codes.Unavailable, "dial workspace %q at %s: %v", ws, addr, err)
	}

	// Forward the caller's metadata verbatim (rat-callmeta-bin identity + trace) so the workspace
	// authorizes the ORIGINAL caller. The hub adds no identity of its own (Q03).
	outCtx := ctx
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		outCtx = metadata.NewOutgoingContext(ctx, md.Copy())
	}
	outCtx, cancel := context.WithCancel(outCtx)
	defer cancel()

	clientStream, err := grpc.NewClientStream(outCtx,
		&grpc.StreamDesc{ServerStreams: true, ClientStreams: true}, conn, fullMethod, grpc.ForceCodec(proxyCodec{}))
	if err != nil {
		return err
	}

	// Relay both directions concurrently (handles unary + all streaming cardinalities). When the
	// caller finishes sending, half-close the upstream; when the workspace finishes, propagate its
	// trailer + status to the caller.
	s2c := forwardFrames(serverStream, clientStream)
	c2s := forwardFrames(clientStream, serverStream)
	for i := 0; i < 2; i++ {
		select {
		case err := <-s2c:
			if err == io.EOF {
				_ = clientStream.CloseSend()
			} else {
				cancel()
				return status.Errorf(codes.Internal, "hub relay (request): %v", err)
			}
		case err := <-c2s:
			serverStream.SetTrailer(clientStream.Trailer())
			if err != io.EOF {
				return err // propagate the workspace's status to the caller
			}
			return nil
		}
	}
	return status.Error(codes.Internal, "hub relay: unreachable")
}

// frameStream is the SendMsg/RecvMsg subset forwardFrames needs (both grpc.ServerStream and
// grpc.ClientStream satisfy it).
type frameStream interface {
	SendMsg(m any) error
	RecvMsg(m any) error
}

// forwardFrames copies opaque frames src→dst until src ends (io.EOF) or errors, reporting the result
// on the returned channel. One goroutine per direction.
func forwardFrames(src, dst frameStream) chan error {
	ret := make(chan error, 1)
	go func() {
		for {
			var f []byte
			if err := src.RecvMsg(&f); err != nil {
				ret <- err // io.EOF on a clean end, or a status error
				return
			}
			if err := dst.SendMsg(f); err != nil {
				ret <- err
				return
			}
		}
	}()
	return ret
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
	pool := newConnPool()
	defer pool.closeAll()
	hub := &hubServer{identityAddr: *identityAddr, pool: pool, resolve: workspaceAddr}
	// A TRANSPARENT proxy: no service is registered; every method routes through the
	// UnknownServiceHandler, and ForceServerCodec relays frames verbatim (ADR-033 Q02).
	opts := []grpc.ServerOption{
		grpc.ForceServerCodec(proxyCodec{}),
		grpc.UnknownServiceHandler(hub.proxyStream),
	}
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
