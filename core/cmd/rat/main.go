// Command rat is the runnable core daemon (ADR-019): `rat serve` turns the sealed
// Phase-1 core — registry + capability-invoke gateway + supervisor + deployment-
// runtime + reconciler — from a tested library into a server a client can connect
// to. It reads a plane.yaml (the desired plugin set), brings the plugins up through
// the deployment-runtime (supervisor.BringUp), serves the gateway over TCP, and
// drains cleanly on SIGTERM/SIGINT. This is the first time the core runs as a server.
//
// Phase A (this build) is launch mode on the local-process runtime, proven on the
// core's Go test plugins. Phase B containerizes the data-dev Python plugins and runs
// them through this same gateway; Phase C adds the attach-mode compose stack.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rat-dev/rat/core/deploymentruntime"
	"github.com/rat-dev/rat/core/gateway"
	"github.com/rat-dev/rat/core/lease"
	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/reconciler"
	"github.com/rat-dev/rat/core/registry"
	"github.com/rat-dev/rat/core/supervisor"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	"google.golang.org/grpc"
)

// shutdownGrace bounds the drain: GracefulStop waits for in-flight calls, then the
// plane tears down provider conns + kills launched instances.
const shutdownGrace = 15 * time.Second

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetPrefix("rat serve: ")

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	planePath := fs.String("plane", "plane.yaml", "path to the plane file (the desired plugin set)")

	// `rat serve [flags]` — serve is the only verb in Phase A. Tolerate it being
	// present or absent so both `rat serve --plane …` and `rat --plane …` work.
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	_ = fs.Parse(args)

	if err := serve(*planePath); err != nil {
		log.Fatal(err)
	}
}

// serve runs the daemon until a signal arrives, then drains. It returns an error
// only on a boot/serve failure; a clean signal-driven drain returns nil.
func serve(planePath string) error {
	// The signal context governs the SERVING lifetime; boot + drain use their own
	// contexts so they aren't cancelled by the very signal we want to drain on.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pl, err := LoadPlane(planePath)
	if err != nil {
		return err
	}
	rt, err := newRuntime(pl.Runtime)
	if err != nil {
		return err
	}
	auditor := NewStdoutAuditor(os.Stdout)

	plane, err := assemble(pl, rt, auditor)
	if err != nil {
		return err
	}

	srv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(srv, plane.Gateway)
	lis, err := net.Listen("tcp", pl.Addr)
	if err != nil {
		drain(srv, plane)
		return fmt.Errorf("listen on %s: %w", pl.Addr, err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	log.Printf("gateway serving on %s — %d plugin(s) up; Ctrl-C / SIGTERM to drain", lis.Addr(), len(pl.Specs))

	select {
	case <-ctx.Done():
		log.Print("signal received — draining")
		drain(srv, plane)
		return nil
	case err := <-serveErr:
		drain(srv, plane)
		if err != nil {
			return fmt.Errorf("gateway serve: %w", err)
		}
		return nil
	}
}

// runningPlane is the gateway the daemon serves + a teardown func. Both modes — launch
// (reconciler-driven) and attach (dial pre-running) — produce one, so serve()/drain() are
// mode-agnostic.
type runningPlane struct {
	Gateway  *gateway.Gateway
	shutdown func(context.Context)
}

func (rp *runningPlane) Shutdown(ctx context.Context) { rp.shutdown(ctx) }

// drain stops the gateway gracefully (in-flight calls finish, no new ones accepted),
// then tears the plane down (stop the loop, terminate instances, close provider conns).
func drain(srv *grpc.Server, plane *runningPlane) {
	srv.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	plane.Shutdown(ctx)
	log.Print("drained")
}

// assemble brings the plane up in the right mode: launch (rat is the SOLE launcher —
// the reconciler launches + supervises + re-wires plugins, ADR-022) or attach (rat dials
// already-running plugins; compose orchestrates them — no docker-in-docker). A v1 plane
// is all-launch or all-attach; mixing is rejected.
func assemble(pl *Plane, rt deploymentruntimev1.DeploymentRuntimeServiceServer, auditor *StdoutAuditor) (*runningPlane, error) {
	var hasLaunch, hasEndpoint bool
	for _, s := range pl.Specs {
		if s.Launch != nil {
			hasLaunch = true
		}
		if s.Endpoint != "" {
			hasEndpoint = true
		}
	}
	switch {
	case hasLaunch && hasEndpoint:
		return nil, fmt.Errorf("plane mixes launch and attach plugins — not supported in v1 (use all-launch or all-attach)")
	case hasEndpoint:
		log.Printf("attaching to %d plugin(s) (health timeout %s)", len(pl.Specs), pl.HealthTimeout)
		p, err := supervisor.Attach(context.Background(), pl.Specs, auditor, pl.HealthTimeout, routableDescriptors()...)
		if err != nil {
			return nil, fmt.Errorf("attach plane: %w", err)
		}
		return &runningPlane{Gateway: p.Gateway, shutdown: p.Shutdown}, nil
	default:
		return launchPlane(pl, rt, auditor)
	}
}

// launchPlane is the launch-mode assembly (ADR-022): rat is the sole launcher. It builds
// the registry + an EMPTY gateway, then runs the reconciler over the desired set with a
// gatewayRewire — the reconciler launches each plugin and, on Healthy, dials it and
// SetProvider's it on the gateway; on crash it relaunches + re-wires (self-healing). It
// waits for the initial set to come up so the gateway is wired before we serve.
func launchPlane(pl *Plane, rt deploymentruntimev1.DeploymentRuntimeServiceServer, auditor *StdoutAuditor) (*runningPlane, error) {
	// Tell each launched plugin how to call the gateway BACK (driver plugins like the
	// scheduler/bff dial rat). The reachable self-address differs by mode — host.containers
	// .internal (podman on the host), rat's own name on the shared net (sibling/socket-mount),
	// or loopback (local processes) — so rat computes it and injects RAT_GATEWAY as a DEFAULT
	// (an explicit plane env still wins). This is why the SAME plugins.yaml runs host-mode and
	// socket-mounted unchanged: the one address that depends on the topology is supplied here.
	gwAddr := selfGatewayAddr(pl)
	manifests := make([]*manifest.Manifest, 0, len(pl.Specs))
	desired := make([]reconciler.Desired, 0, len(pl.Specs))
	for _, s := range pl.Specs {
		manifests = append(manifests, s.Manifest)
		if s.Launch != nil {
			if s.Launch.Env == nil {
				s.Launch.Env = map[string]string{}
			}
			if _, set := s.Launch.Env["RAT_GATEWAY"]; !set {
				s.Launch.Env["RAT_GATEWAY"] = gwAddr
			}
			// rat knows each plugin's name (its manifest name) — inject it as the caller
			// identity a plugin presents when IT calls the gateway (e.g. rat-pipeline
			// fetching its applied project via state/get). A default; an explicit env wins.
			if _, set := s.Launch.Env["RAT_PLUGIN_NAME"]; !set {
				s.Launch.Env["RAT_PLUGIN_NAME"] = s.Manifest.Metadata.Name
			}
			desired = append(desired, reconciler.Desired{Name: s.Manifest.Metadata.Name, Launch: s.Launch})
		}
	}
	log.Printf("plugins dial the gateway back at %s (injected RAT_GATEWAY)", gwAddr)
	reg, err := registry.New(manifests)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	gw := gateway.New(reg, nil, auditor, routableDescriptors()...)
	rewire := newGatewayRewire(gw)
	rec := reconciler.New(rt, desired, reconciler.Config{
		BaseBackoff:      500 * time.Millisecond,
		MaxBackoff:       10 * time.Second,
		CrashLoopCap:     6,
		ReadinessTimeout: pl.HealthTimeout,
		Rewire:           rewire,
	})
	loopCtx, cancelLoop := context.WithCancel(context.Background())
	loop := &reconciler.Loop{
		Elector:    lease.NewElector("rat-serve", lease.NewStore(), 10*time.Second),
		Reconciler: rec,
		Tick:       200 * time.Millisecond,
	}
	go loop.Run(loopCtx)
	log.Printf("launching %d plugin(s) via the %q runtime (reconciler-driven; self-heals on crash)", len(desired), pl.Runtime)

	// Wait for the initial desired set to be Healthy (Bound) so the gateway is wired before
	// we serve — the same "ready when serving" semantics as the old static bring-up.
	deadline := time.Now().Add(pl.HealthTimeout + 5*time.Second)
	for _, d := range desired {
		for {
			if st, _, _ := rec.Status(d.Name); st == reconciler.Healthy {
				break
			}
			if time.Now().After(deadline) {
				cancelLoop()
				rec.Shutdown(context.Background())
				rewire.Close()
				return nil, fmt.Errorf("plugin %q never became healthy within %s", d.Name, pl.HealthTimeout)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	shutdown := func(ctx context.Context) {
		cancelLoop()      // stop the reconcile loop (so no pass races the teardown)
		rec.Shutdown(ctx) // terminate launched instances
		rewire.Close()    // close the gateway provider conns
	}
	return &runningPlane{Gateway: gw, shutdown: shutdown}, nil
}

// selfGatewayAddr computes the address a LAUNCHED plugin uses to call the gateway back,
// for the current topology. Sibling/socket-mount (rat is itself a container on a shared
// net): rat's own hostname (== its container name, resolvable via podman DNS). Podman on
// the host: host.containers.internal (the host gateway, where rat listens on 0.0.0.0).
// Local processes: loopback. The port is the gateway's own listen port.
func selfGatewayAddr(pl *Plane) string {
	_, port, err := net.SplitHostPort(pl.Addr)
	if err != nil || port == "" {
		port = "7777"
	}
	switch {
	case os.Getenv("RAT_PODMAN_NETWORK") != "":
		h, _ := os.Hostname()
		if h == "" {
			h = "rat"
		}
		return h + ":" + port
	case pl.Runtime == "podman":
		return "host.containers.internal:" + port
	default:
		return "127.0.0.1:" + port
	}
}

// newRuntime selects the deployment-runtime axis plugin the plane asks for. Phase A
// defaults to local-process (the `chmod +x ./rat` runtime); podman is the B/C path.
func newRuntime(name string) (deploymentruntimev1.DeploymentRuntimeServiceServer, error) {
	switch name {
	case "local":
		return deploymentruntime.NewLocalProcess(), nil
	case "podman":
		// $RAT_PODMAN_NETWORK switches the podman runtime to SIBLING mode (ADR-022
		// socket-mount): rat is itself a container driving the host's podman over a
		// mounted socket, so it launches plugins onto a shared network and dials them by
		// name. Unset (rat on the host) → the default host-publish behavior. The SAME
		// plane file works both ways; only this environment knob differs.
		if net := os.Getenv("RAT_PODMAN_NETWORK"); net != "" {
			return deploymentruntime.NewPodmanNetworked(net), nil
		}
		return deploymentruntime.NewPodman(), nil
	default:
		return nil, fmt.Errorf("unknown runtime %q", name)
	}
}
