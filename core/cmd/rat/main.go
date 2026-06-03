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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rat-dev/rat/core/client"
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

// version is the build version, injected at release time via
// -ldflags "-X main.version=<tag>" (the GHCR release pipeline, Phase 4). "dev" for a
// local build.
var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	// rat is a multi-call binary (ADR-023): daemon verbs (serve/up) + project verbs
	// (init/add). A leading non-flag token is the subcommand; legacy `rat --plane …`
	// (no subcommand) still serves.
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		if args[0] == "--version" || args[0] == "-v" {
			cmd, args = "version", args[1:]
		} else if !strings.HasPrefix(args[0], "-") {
			cmd, args = args[0], args[1:]
		}
	}

	var err error
	switch cmd {
	case "", "serve":
		log.SetPrefix("rat serve: ")
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		planePath := fs.String("plane", "plane.yaml", "path to the plane file (the desired plugin set)")
		_ = fs.Parse(args)
		err = serve(*planePath)
	case "up":
		log.SetPrefix("rat: ")
		err = runUp(args, os.Stdout)
	case "down":
		err = runDown(args, os.Stdout)
	case "status":
		err = runStatus(args, os.Stdout)
	case "ls":
		err = runLs(args, os.Stdout)
	case "init":
		err = runInit(args, os.Stdout)
	case "add":
		err = runAdd(args, os.Stdout)
	case "remove", "rm":
		// poetry-style: drop a plugin from rat.toml (the inverse of `rat add`).
		err = runRemove(args, os.Stdout)
	case "call", "apply":
		// the client verbs (the kubectl side), shared with the ratctl alias (ADR-023).
		err = client.Run(append([]string{cmd}, args...), os.Stdout)
	case "version":
		fmt.Printf("rat %s\n", version)
	case "ui":
		// the CLI SURFACE consumer (ADR-025): render/run the cli-targeted contributions.
		err = client.RunUI(args, os.Stdout)
	case "plugin":
		// the plugin authoring toolkit (ADR-026): init | check | test | pack | publish.
		err = runPlugin(args, os.Stdout)
	case "search":
		// discover plugins across local + added marketplaces (the marketplace axis).
		err = runSearch(args, os.Stdout)
	case "list":
		// the plugins installed in this project (rat.toml).
		err = runList(args, os.Stdout)
	case "marketplace", "market":
		// manage the added marketplaces (add | list).
		err = runMarketplace(args, os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q (want: serve | up | down | status | ls | init | add | call | apply | ui | plugin | version)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rat:", err)
		os.Exit(1)
	}
}

// serve loads a YAML plane file and runs the daemon (`rat serve --plane …`, the low-level
// path beneath the poetry verbs).
func serve(planePath string) error {
	pl, err := LoadPlane(planePath)
	if err != nil {
		return err
	}
	return serveResolved(pl)
}

// serveResolved runs the daemon for an already-loaded Plane until a signal arrives, then
// drains. Shared by `rat serve` (YAML plane) and `rat up` (TOML project). Returns an error
// only on a boot/serve failure; a clean signal-driven drain returns nil.
func serveResolved(pl *Plane) error {
	// The signal context governs the SERVING lifetime; boot + drain use their own
	// contexts so they aren't cancelled by the very signal we want to drain on.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := newRuntime(pl.Runtime, pl.Instance)
	if err != nil {
		return err
	}
	auditor := NewStdoutAuditor(os.Stdout)

	// Open the listeners BEFORE assembling, so the gateway-callback address (the port a
	// launched DRIVER plugin dials back on) is known in time to inject RAT_GATEWAY. The
	// control listener is pl.Addr — a per-project unix socket (ADR-023) or TCP. A unix
	// socket is unreachable by launched plugins across the container/host boundary, so when
	// control is a socket we ALSO open an auto-port TCP COMPANION for callbacks (":0" → a
	// free port, collision-free across instances). When control is already TCP, it doubles
	// as the callback endpoint.
	ctlLis, err := listen(pl.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", pl.Addr, err)
	}
	var cbLis net.Listener
	cbPort := tcpPort(ctlLis) // "" when the control listener is a unix socket
	if cbPort == "" {
		cbLis, err = net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			_ = ctlLis.Close()
			return fmt.Errorf("open plugin-callback listener: %w", err)
		}
		cbPort = tcpPort(cbLis)
	}
	pl.CallbackAddr = gatewayCallbackAddr(pl, cbPort)

	plane, err := assemble(pl, rt, auditor)
	if err != nil {
		_ = ctlLis.Close()
		if cbLis != nil {
			_ = cbLis.Close()
		}
		return err
	}

	srv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(srv, plane.Gateway)

	// Publish this daemon's pid + global registry entry (slice 2c) so `rat down`/`ls`/
	// `status` can find it; retract both on drain. No-op when the plane has no project
	// (.rat/) context — a raw `rat serve --plane …`.
	registerDaemon(pl)
	defer deregisterDaemon(pl)

	// Serve the same gateway on both listeners. GracefulStop in drain() closes both.
	serveErr := make(chan error, 2)
	go func() { serveErr <- srv.Serve(ctlLis) }()
	cbDesc := "(same as control)"
	if cbLis != nil {
		go func() { serveErr <- srv.Serve(cbLis) }()
		cbDesc = cbLis.Addr().String()
	}
	log.Printf("gateway serving — control %s · plugin-callbacks %s — %d plugin(s) up; Ctrl-C / SIGTERM to drain",
		ctlLis.Addr(), cbDesc, len(pl.Specs))

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

// tcpPort returns the port of a TCP listener as a string, or "" for a non-TCP (unix)
// listener.
func tcpPort(lis net.Listener) string {
	if a, ok := lis.Addr().(*net.TCPAddr); ok {
		return strconv.Itoa(a.Port)
	}
	return ""
}

// listen opens the daemon's control listener. A "unix:<path>" address binds a per-project
// UNIX SOCKET (ADR-023): many rat daemons coexist on one machine with no port war, and
// filesystem permissions are the access control. Any other address is a TCP host:port
// (":0" auto-assigns a free port — the collision-free alternative when a network endpoint
// is actually needed). For a unix socket we ensure the parent dir exists and remove a stale
// socket a crashed prior daemon may have left.
func listen(addr string) (net.Listener, error) {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		path = strings.TrimPrefix(path, "//") // tolerate unix://<path> and unix:<path>
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create socket dir: %w", err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove stale socket %s: %w", path, err)
		}
		return net.Listen("unix", path) // Go unlinks the socket on Close (SetUnlinkOnClose default)
	}
	return net.Listen("tcp", addr)
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
	gwAddr := pl.CallbackAddr
	if gwAddr == "" {
		gwAddr = selfGatewayAddr(pl) // fallback (e.g. a direct launchPlane in a test)
	}
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
	logUnsatisfied(manifests) // poetry-style: warn about any `requires` no plugin provides
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

// gatewayCallbackAddr computes the address a LAUNCHED plugin uses to call the gateway back,
// for the current topology, at the given callback port. Sibling/socket-mount (rat is itself
// a container on a shared net): rat's own hostname (== its container name, resolvable via
// podman DNS). Podman on the host: host.containers.internal (the host gateway, where the
// callback listener binds 0.0.0.0). Local processes: loopback.
func gatewayCallbackAddr(pl *Plane, port string) string {
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

// selfGatewayAddr is the fallback callback address derived from pl.Addr's port (used when
// serve() hasn't set pl.CallbackAddr precisely — e.g. a direct launchPlane in a test).
func selfGatewayAddr(pl *Plane) string {
	_, port, err := net.SplitHostPort(pl.Addr)
	if err != nil || port == "" {
		port = "7777"
	}
	return gatewayCallbackAddr(pl, port)
}

// newRuntime selects the deployment-runtime axis plugin the plane asks for. Phase A
// defaults to local-process (the `chmod +x ./rat` runtime); podman is the B/C path. The
// instance id (ADR-023) namespaces SIBLING-mode runtime resources so many rats coexist.
func newRuntime(name, instance string) (deploymentruntimev1.DeploymentRuntimeServiceServer, error) {
	switch name {
	case "local":
		return deploymentruntime.NewLocalProcess(), nil
	case "podman":
		// SIBLING mode (ADR-022 socket-mount): rat is itself a container driving the host's
		// podman over a mounted socket, so it launches plugins onto the shared network
		// $RAT_PODMAN_NETWORK (the operator gives each rat its own) and dials them by name.
		// To let MANY rats coexist (ADR-023), container names are prefixed with the instance
		// id — so two daemons never collide on a name like "rat-state-1" even if they share a
		// network. Host mode (no network) publishes to ephemeral loopback ports + lets podman
		// auto-name, which already coexists.
		if net := os.Getenv("RAT_PODMAN_NETWORK"); net != "" {
			return deploymentruntime.NewPodmanInstanced(net, instance), nil
		}
		return deploymentruntime.NewPodman(), nil
	default:
		return nil, fmt.Errorf("unknown runtime %q", name)
	}
}
