// Package supervisor brings up a set of plugins through a deployment-runtime and
// wires them behind the capability-invoke gateway (ADR-016): manifests -> Launch ->
// healthcheck -> dial -> register -> gateway. It replaces the spike's "dial
// pre-running providers" — provider connections now come from launched (isolated)
// processes the core itself brought up.
package supervisor

import (
	"context"
	"fmt"
	"time"

	"github.com/rat-dev/rat/core/gateway"
	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/registry"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// PluginSpec is one plugin to bring up. Launch is nil for a caller/driver that is
// only registered (so the gateway knows its `requires` for C5) and is not itself
// launched as a provider.
type PluginSpec struct {
	Manifest *manifest.Manifest
	Launch   *deploymentruntimev1.LaunchSpec
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
