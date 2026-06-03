// Package supervisor brings up a set of plugins through a deployment-runtime and
// wires them behind the capability-invoke gateway (ADR-016): manifests -> Launch ->
// healthcheck -> dial -> register -> gateway. It replaces the spike's "dial
// pre-running providers" — provider connections now come from launched (isolated)
// processes the core itself brought up.
package supervisor

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rat-dev/rat/core/gateway"
	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/registry"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// PluginSpec is one plugin in a plane. Exactly one of Launch / Endpoint is set for a
// provider; both nil/empty means a caller/driver that is only registered (so the
// gateway knows its `requires` for C5) and is neither launched nor dialed.
//
//   - Launch set   → launch mode (BringUp launches + supervises it).
//   - Endpoint set → attach mode (Attach dials an already-running plugin at this addr).
type PluginSpec struct {
	Manifest *manifest.Manifest
	Launch   *deploymentruntimev1.LaunchSpec
	Endpoint string
}

// Plane is a running set of launched plugins behind the gateway.
type Plane struct {
	Gateway  *gateway.Gateway
	Registry *registry.Registry

	runtime   deploymentruntimev1.DeploymentRuntimeServiceServer
	instances []string
	conns     []*grpc.ClientConn
}

// BringUp launches every spec that carries a Launch (waiting until healthy, then
// dialing it), registers ALL manifests (launched providers + caller/driver specs),
// and constructs the gateway over the launched providers. On any failure it tears
// down whatever already came up. The caller owns Shutdown.
func BringUp(
	ctx context.Context,
	runtime deploymentruntimev1.DeploymentRuntimeServiceServer,
	specs []PluginSpec,
	auditor gateway.Auditor,
	healthTimeout time.Duration,
	descriptors ...protoreflect.FileDescriptor,
) (*Plane, error) {
	p := &Plane{runtime: runtime}
	manifests := make([]*manifest.Manifest, 0, len(specs))
	providers := map[string]*grpc.ClientConn{}

	for _, s := range specs {
		manifests = append(manifests, s.Manifest)
		if s.Launch == nil {
			continue // registered only (a caller/driver), not a launched provider
		}
		name := s.Manifest.Metadata.Name
		lr, err := runtime.Launch(ctx, &deploymentruntimev1.LaunchRequest{PluginId: name, Spec: s.Launch})
		if err != nil {
			p.Shutdown(ctx)
			return nil, fmt.Errorf("launch %q: %w", name, err)
		}
		p.instances = append(p.instances, lr.GetInstanceId())
		if err := waitHealthy(ctx, runtime, lr.GetInstanceId(), healthTimeout); err != nil {
			p.Shutdown(ctx)
			return nil, fmt.Errorf("plugin %q never became healthy: %w", name, err)
		}
		conn, err := grpc.NewClient(lr.GetEndpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			p.Shutdown(ctx)
			return nil, fmt.Errorf("dial %q at %s: %w", name, lr.GetEndpoint(), err)
		}
		p.conns = append(p.conns, conn)
		providers[name] = conn
	}

	reg, err := registry.New(manifests)
	if err != nil {
		p.Shutdown(ctx)
		return nil, fmt.Errorf("registry: %w", err)
	}
	p.Registry = reg
	p.Gateway = gateway.New(reg, providers, auditor, descriptors...)
	return p, nil
}

// Shutdown closes provider connections and terminates every launched instance.
// Safe to call more than once.
func (p *Plane) Shutdown(ctx context.Context) {
	for _, c := range p.conns {
		_ = c.Close()
	}
	for _, id := range p.instances {
		_, _ = p.runtime.Terminate(ctx, &deploymentruntimev1.TerminateRequest{InstanceId: id})
	}
	p.conns = nil
	p.instances = nil
}

// Attach builds a Plane over ALREADY-RUNNING plugins (ADR-019 attach mode / ADR-020
// S1): it dials each spec's Endpoint (waiting until it accepts, up to healthTimeout),
// registers ALL manifests (attached providers + caller/driver specs), and constructs
// the gateway over the dialed providers. Unlike BringUp it launches NOTHING — the
// orchestrator (e.g. compose) starts the plugins; the daemon connects by address, so
// the daemon can itself run in a container with no docker-in-docker. The caller owns
// Shutdown (which closes the dialed conns; there are no launched instances to kill).
func Attach(
	ctx context.Context,
	specs []PluginSpec,
	auditor gateway.Auditor,
	healthTimeout time.Duration,
	descriptors ...protoreflect.FileDescriptor,
) (*Plane, error) {
	p := &Plane{} // no runtime — Attach launches nothing
	manifests := make([]*manifest.Manifest, 0, len(specs))
	providers := map[string]*grpc.ClientConn{}

	for _, s := range specs {
		manifests = append(manifests, s.Manifest)
		if s.Endpoint == "" {
			continue // registered only (a caller/driver), not an attached provider
		}
		name := s.Manifest.Metadata.Name
		// Compose may start the daemon before the plugin is accepting; wait for it.
		if err := waitEndpoint(ctx, s.Endpoint, healthTimeout); err != nil {
			p.Shutdown(ctx)
			return nil, fmt.Errorf("plugin %q endpoint %s never became reachable: %w", name, s.Endpoint, err)
		}
		conn, err := grpc.NewClient(s.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			p.Shutdown(ctx)
			return nil, fmt.Errorf("dial %q at %s: %w", name, s.Endpoint, err)
		}
		p.conns = append(p.conns, conn)
		providers[name] = conn
	}

	reg, err := registry.New(manifests)
	if err != nil {
		p.Shutdown(ctx)
		return nil, fmt.Errorf("registry: %w", err)
	}
	p.Registry = reg
	p.Gateway = gateway.New(reg, providers, auditor, descriptors...)
	return p, nil
}

// waitEndpoint polls a TCP endpoint until it accepts a connection or timeout elapses.
func waitEndpoint(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func waitHealthy(ctx context.Context, runtime deploymentruntimev1.DeploymentRuntimeServiceServer, id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		hc, err := runtime.Healthcheck(ctx, &deploymentruntimev1.HealthcheckRequest{InstanceId: id})
		if err != nil {
			return err
		}
		if hc.GetStatus() == deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("status=%s detail=%q", hc.GetStatus(), hc.GetDetail())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
