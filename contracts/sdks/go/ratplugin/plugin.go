// Package ratplugin is the rat plugin runtime SDK (ADR-029): the thin helper layer that kills the
// serve + consume boilerplate. It lives in the gen module, so it rides in the plugin-base-go image
// (the SDK at /sdk) — a Go plugin imports "github.com/le-squat/rat/gen/ratplugin" with no setup.
//
//	func main() {
//	    k := &keyring{secrets: ratplugin.EnvMap("RAT_SECRETS")}
//	    ratplugin.Serve(func(s grpc.ServiceRegistrar) { secretv1.RegisterSecretServiceServer(s, k) })
//	}
package ratplugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	commonv1 "github.com/le-squat/rat/gen/rat/common/v1"
	corev1 "github.com/le-squat/rat/gen/rat/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// Serve runs a plugin's gRPC server until SIGTERM, then drains gracefully. It reads
// RAT_PLUGIN_ADDR (the address the deployment-runtime told the plugin to bind), lets the caller
// register one OR MANY servicers in the closure, and blocks. The whole serving dance, once.
func Serve(register func(grpc.ServiceRegistrar)) {
	addr := envOr("RAT_PLUGIN_ADDR", "0.0.0.0:50051")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("ratplugin: listen %s: %v", addr, err)
	}
	s := grpc.NewServer()
	register(s)
	go func() {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		<-ctx.Done()
		stop()
		s.GracefulStop()
	}()
	log.Printf("ratplugin: %s serving on %s", os.Getenv("RAT_PLUGIN_NAME"), addr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("ratplugin: serve: %v", err)
	}
}

// EnvMap parses a "k=v,k=v" env var (the RAT_SECRETS / config convention) into a map.
func EnvMap(name string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(os.Getenv(name), ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[strings.TrimSpace(k)] = v
		}
	}
	return out
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// Client calls capabilities through the gateway. Build it once with Gateway().
type Client struct {
	conn   *grpc.ClientConn
	invoke corev1.CapabilityInvokeServiceClient
	name   string // this plugin's caller identity (RAT_PLUGIN_NAME)
}

// Gateway dials the gateway at RAT_GATEWAY (injected by rat) and returns a Client. The dial is
// lazy (grpc.NewClient), so this is cheap to call at startup.
func Gateway() *Client {
	addr := envOr("RAT_GATEWAY", "127.0.0.1:7777")
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("ratplugin: dial gateway %s: %v", addr, err)
	}
	return &Client{conn: conn, invoke: corev1.NewCapabilityInvokeServiceClient(conn), name: os.Getenv("RAT_PLUGIN_NAME")}
}

// Call invokes a capability through the gateway: it stamps the rat-callmeta-bin envelope (this
// plugin's identity + a fresh traceparent, ADR-007), marshals req, Invokes, and unmarshals into
// resp. Tenant is propagated from ctx if it carries an incoming envelope, else "default".
func (c *Client) Call(ctx context.Context, capability string, req, resp proto.Message) error {
	payload, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	tenant := CallerTenant(ctx)
	if tenant == "" {
		tenant = "default"
	}
	rc := &commonv1.RequestContext{
		Trace:    &commonv1.TraceContext{Traceparent: newTraceparent(), CorrelationId: c.name},
		Identity: &commonv1.Identity{CallerPlugin: c.name, Tenant: tenant},
	}
	b, _ := proto.Marshal(rc)
	octx := metadata.AppendToOutgoingContext(ctx, "rat-callmeta-bin", string(b))
	out, err := c.invoke.Invoke(octx, &corev1.InvokeRequest{Capability: capability, Payload: payload})
	if err != nil {
		return err
	}
	return proto.Unmarshal(out.GetResult(), resp)
}

// Close releases the gateway connection.
func (c *Client) Close() error { return c.conn.Close() }

// CallerTenant reads the calling tenant out of the INCOMING rat-callmeta-bin envelope (for C7
// tenant-scoping). Returns "" if there is none.
func CallerTenant(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("rat-callmeta-bin")
	if len(vals) == 0 {
		return ""
	}
	var rc commonv1.RequestContext
	if proto.Unmarshal([]byte(vals[0]), &rc) != nil {
		return ""
	}
	return rc.GetIdentity().GetTenant()
}

// newTraceparent makes a valid W3C traceparent: 00-<32 hex>-<16 hex>-01.
func newTraceparent() string {
	var trace [16]byte
	var span [8]byte
	_, _ = rand.Read(trace[:])
	_, _ = rand.Read(span[:])
	return "00-" + hex.EncodeToString(trace[:]) + "-" + hex.EncodeToString(span[:]) + "-01"
}
