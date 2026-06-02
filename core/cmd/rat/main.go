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

	plane, err := assemble(context.Background(), pl, rt, auditor)
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

// drain stops the gateway gracefully (in-flight calls finish, no new ones accepted),
// then tears the plane down (close provider conns + terminate launched instances).
func drain(srv *grpc.Server, plane *supervisor.Plane) {
	srv.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	plane.Shutdown(ctx)
	log.Print("drained")
}

// assemble brings the plane up in the right mode: launch (the daemon launches +
// supervises plugins) or attach (the daemon dials already-running plugins — the
// orchestrator, e.g. compose, started them; no docker-in-docker). A v1 plane is
// all-launch or all-attach; mixing is rejected.
func assemble(ctx context.Context, pl *Plane, rt deploymentruntimev1.DeploymentRuntimeServiceServer, auditor *StdoutAuditor) (*supervisor.Plane, error) {
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
		p, err := supervisor.Attach(ctx, pl.Specs, auditor, pl.HealthTimeout, routableDescriptors()...)
		if err != nil {
			return nil, fmt.Errorf("attach plane: %w", err)
		}
		return p, nil
	default:
		log.Printf("bringing up %d plugin(s) via the %q runtime (health timeout %s)", len(pl.Specs), pl.Runtime, pl.HealthTimeout)
		p, err := supervisor.BringUp(ctx, rt, pl.Specs, auditor, pl.HealthTimeout, routableDescriptors()...)
		if err != nil {
			return nil, fmt.Errorf("bring up plane: %w", err)
		}
		return p, nil
	}
}

// newRuntime selects the deployment-runtime axis plugin the plane asks for. Phase A
// defaults to local-process (the `chmod +x ./rat` runtime); podman is the B/C path.
func newRuntime(name string) (deploymentruntimev1.DeploymentRuntimeServiceServer, error) {
	switch name {
	case "local":
		return deploymentruntime.NewLocalProcess(), nil
	case "podman":
		return deploymentruntime.NewPodman(), nil
	default:
		return nil, fmt.Errorf("unknown runtime %q", name)
	}
}
