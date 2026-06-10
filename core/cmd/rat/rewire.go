package main

import (
	"log"
	"sync"

	"github.com/squat-collective/rat-v3/core/gateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// gatewayRewire implements reconciler.Rewire by (re)binding a plugin's live connection on
// the gateway as the reconciler launches / relaunches / loses it (ADR-022). It is the seam
// that makes `rat serve` self-heal routing: when a crashed plugin relaunches on a NEW
// endpoint, the reconciler calls Bind, and the gateway re-dials there — calls in flight to
// the old conn drain, new calls route to the new one.
type gatewayRewire struct {
	gw    *gateway.Gateway
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

func newGatewayRewire(gw *gateway.Gateway) *gatewayRewire {
	return &gatewayRewire{gw: gw, conns: map[string]*grpc.ClientConn{}}
}

// Bind dials the plugin's endpoint and SetProvider's it on the gateway, closing any prior
// connection it replaces. (grpc.NewClient is lazy — the plugin is already Healthy by the
// time the reconciler calls this, so the dial is sound.)
func (g *gatewayRewire) Bind(name, endpoint string) {
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("rewire: dial %s at %s failed: %v", name, endpoint, err)
		return
	}
	if prev := g.gw.SetProvider(name, conn); prev != nil {
		_ = prev.Close()
	}
	g.mu.Lock()
	g.conns[name] = conn
	g.mu.Unlock()
	log.Printf("wired %s -> %s", name, endpoint)
}

// Unbind drops the plugin from routing (RemoveProvider) and closes its connection.
func (g *gatewayRewire) Unbind(name string) {
	if prev := g.gw.RemoveProvider(name); prev != nil {
		_ = prev.Close()
	}
	g.mu.Lock()
	delete(g.conns, name)
	g.mu.Unlock()
	log.Printf("unwired %s", name)
}

// Close closes every live provider connection (daemon shutdown).
func (g *gatewayRewire) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.conns {
		_ = c.Close()
	}
	g.conns = map[string]*grpc.ClientConn{}
}
