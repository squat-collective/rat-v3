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
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/squat-collective/rat-v3/core/client"
	"github.com/squat-collective/rat-v3/core/deploymentruntime"
	"github.com/squat-collective/rat-v3/core/gateway"
	"github.com/squat-collective/rat-v3/core/lease"
	"github.com/squat-collective/rat-v3/core/manifest"
	"github.com/squat-collective/rat-v3/core/metrics"
	"github.com/squat-collective/rat-v3/core/reconciler"
	"github.com/squat-collective/rat-v3/core/registry"
	"github.com/squat-collective/rat-v3/core/supervisor"
	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	deploymentruntimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/deploymentruntime/v1"
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
		switch {
		case args[0] == "--version" || args[0] == "-v":
			cmd, args = "version", args[1:]
		case args[0] == "--help" || args[0] == "-h" || args[0] == "help":
			cmd, args = "help", args[1:]
		case !strings.HasPrefix(args[0], "-"):
			cmd, args = args[0], args[1:]
		}
	}

	var err error
	switch cmd {
	case "help":
		printHelp(os.Stdout)
	case "", "serve":
		// Bare `rat` (no subcommand, no flags) shows help — NOT a daemon boot from a
		// missing plane.yaml. `rat serve` (or the legacy `rat --plane …`) starts the daemon.
		if cmd == "" && len(args) == 0 {
			printHelp(os.Stdout)
			break
		}
		log.SetPrefix("rat serve: ")
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		planePath := fs.String("plane", "plane.yaml", "path to the plane file (the desired plugin set)")
		strict := fs.Bool("strict", false, "preflight the plane (rat validate) and refuse to boot on any error")
		_ = fs.Parse(args)
		err = serve(*planePath, *strict)
	case "validate":
		// the static preflight (DX-1): every boot-path check, no boot.
		err = runValidate(args, os.Stdout)
	case "capabilities", "caps":
		// the readable in-binary capability registry (DX-3).
		err = runCapabilities(args, os.Stdout)
	case "up":
		log.SetPrefix("rat: ")
		err = runUp(args, os.Stdout)
	case "down":
		err = runDown(args, os.Stdout)
	case "status":
		err = runStatus(args, os.Stdout)
	case "hub":
		// federation front door (ADR-033): one endpoint fanning out to many workspace daemons.
		log.SetPrefix("rat hub: ")
		err = runHub(args, os.Stdout)
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
	case "context", "ctx":
		// kubectl-style connection profiles: pin {addr, as, token, workspace} so commands
		// target a remote rat without retyping flags (ADR remote-dev-flow).
		err = client.RunContext(args, os.Stdout)
	default:
		// Not a built-in verb → try a plugin-CONTRIBUTED command (ADR-041): the rat CLI is a thin
		// dispatcher; commands like `rat run`/`rat branch` come from plugins, surfaced from the
		// connected (possibly remote) gateway. The leading token may be the first of a multi-token
		// command name (e.g. `branch create`), so pass the whole argv.
		err = client.RunCommand(append([]string{cmd}, args...), os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rat:", err)
		os.Exit(1)
	}
}

// printHelp is what bare `rat` (and `rat help` / `-h`) shows: the one-screen overview of the
// command surface, grouped by what you're doing.
func printHelp(out io.Writer) {
	fmt.Fprintf(out, `rat %s — a data platform that orchestrates self-describing plugins.
The core does six things; everything else is a plugin.

USAGE
  rat <command> [args]        run `+"`rat <command> -h`"+` for a command's flags

PROJECT  (a project is a rat.toml — the declared plugin set)
  init        create a rat.toml here (a new project)
  add         add a plugin to the project (reads the manifest stamped in its image)
  remove      remove a plugin from the project            (alias: rm)
  list        list the plugins installed in this project
  search      find plugins across the local + added marketplaces

DAEMON  (run the project's plane)
  up          start this project's daemon            (-d = background, --strict = preflight)
  down        stop this project's daemon
  status      this project's daemon + its plugins
  validate    preflight a plane/project WITHOUT booting (manifests · deps · images)
  ls          every rat daemon running on this machine
  hub         federate many workspaces behind one endpoint (ADR-033)
  serve       run a daemon directly from a plane.yaml (low-level; --strict = preflight)

AUTHOR  (build a plugin)
  capabilities    list every rat:// capability this rat links   rat capabilities [state|state-backend]
  plugin init     scaffold a plugin   (--kind <axis> --lang go|python|typescript|rust)
  plugin check    validate its manifest (static gate)
  plugin dev      watch the dir; re-run check + test on change (the inner loop)
  plugin pack     build + stamp the manifest + verify it serves what it declares
  plugin publish  push the verified image to a registry

MARKETPLACE  (discover + distribute plugins)
  marketplace add|list          manage plugin sources (local images + remote indexes)
  marketplace keygen|sign|verify  ed25519 provenance for an index

CLIENT  (talk to a running gateway)
  call        invoke a capability         rat call <rat://…> --as <caller> --data <json>
  apply       submit a project (e.g. a dbt project) to the orchestrator
  ui          render a surface's plugin contributions

  version     print the version          help        this screen

Docs: docs/ in the rat repo · ADRs in docs/architecture/adrs/.
`, version)
}

// serve loads a YAML plane file and runs the daemon (`rat serve --plane …`, the low-level
// path beneath the poetry verbs). strict runs the static preflight first and refuses to
// boot on any error (DX-1) — without it, boot problems surface as warnings + Degraded.
func serve(planePath string, strict bool) error {
	pl, err := LoadPlane(planePath)
	if err != nil {
		return err
	}
	if strict {
		if err := strictPreflight(pl, planePath); err != nil {
			return err
		}
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

	// Durable local storage (ADR-031): a project gets a persistent per-plugin /data mount under
	// .rat/data/ (survives restart, gitignored). No project (raw `rat serve --plane`) → ephemeral.
	dataRoot := ""
	if pl.RuntimeDir != "" {
		dataRoot = filepath.Join(pl.RuntimeDir, "data")
	}
	rt, err := newRuntime(pl.Runtime, pl.Instance, dataRoot)
	if err != nil {
		return err
	}
	// Mandatory audit (plugin-architecture.md). It always tees to stdout (container logs); when the
	// project has a runtime dir it ALSO appends to a DURABLE JSONL file (gap #6) that survives
	// restart — so the decision trail isn't lost with the process. No project → stdout only.
	auditW := io.Writer(os.Stdout)
	var auditClose func()
	if pl.RuntimeDir != "" {
		f, ferr := os.OpenFile(filepath.Join(pl.RuntimeDir, "audit.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if ferr != nil {
			return fmt.Errorf("open durable audit log: %w", ferr)
		}
		auditW = io.MultiWriter(os.Stdout, f)
		auditClose = func() { _ = f.Close() }
		log.Printf("durable audit → %s", filepath.Join(pl.RuntimeDir, "audit.jsonl"))
	}
	if auditClose != nil {
		defer auditClose() // flush+close the durable audit file on any exit (after drain)
	}
	auditor := NewStdoutAuditor(auditW)

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
	// Plugin callbacks must reach the gateway from inside a container. They can't when control
	// is a unix socket (cbPort==""), OR when control is TCP bound to LOOPBACK under a container
	// runtime — host.containers.internal can't reach a 127.0.0.1-only listener (Gap 6). In both
	// cases open a 0.0.0.0 companion so launched plugins can dial the gateway back.
	loopbackTCP := cbPort != "" && isLoopbackBind(pl.Addr) && isContainerRuntime(pl.Runtime)
	if cbPort == "" || loopbackTCP {
		cbLis, err = net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			_ = ctlLis.Close()
			return fmt.Errorf("open plugin-callback listener: %w", err)
		}
		cbPort = tcpPort(cbLis)
		if loopbackTCP {
			log.Printf("note: control is loopback (%s) under the %q runtime — opened a 0.0.0.0 plugin-callback companion so launched plugins can reach the gateway (Gap 6)", pl.Addr, pl.Runtime)
		}
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

	// Two doors, two trust models (C2 / ADR-034). The CONTROL listener is the operator door
	// (rat call / the ControlService), trusted by reachability — a per-project unix socket's
	// filesystem perms or an operator TCP endpoint. The plugin-CALLBACK listener is the 0.0.0.0
	// door launched plugins dial back on; it AUTHENTICATES each plugin by its per-launch bearer
	// token (the gateway's PluginAuth interceptors), so a plugin can't forge another's identity.
	ctlSrv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(ctlSrv, plane.Gateway)
	// The live control plane (ADR-027): register/deregister plugins against the running
	// daemon. Launch mode only — rat must be the launcher to materialize a live add.
	if plane.Control != nil {
		corev1.RegisterControlServiceServer(ctlSrv, plane.Control)
	}
	servers := []*grpc.Server{ctlSrv}

	var cbSrv *grpc.Server
	if cbLis != nil {
		cbSrv = grpc.NewServer(
			grpc.UnaryInterceptor(plane.Gateway.PluginAuthUnaryInterceptor),
			grpc.StreamInterceptor(plane.Gateway.PluginAuthStreamInterceptor),
		)
		corev1.RegisterCapabilityInvokeServiceServer(cbSrv, plane.Gateway)
		servers = append(servers, cbSrv)
	}

	// Native observability (plugin-architecture.md / gap #6): a dependency-free metrics registry,
	// fed by the gateway's per-call outcomes and a live plugin-state gauge pulled from the control
	// plane at scrape. Served at /metrics when RAT_METRICS_ADDR is set (core-native — no plugin
	// required; an observability-axis plugin layers richer telemetry on top).
	reg := metrics.NewRegistry()
	plane.Gateway.OnCall = func(capability, outcome string) {
		reg.Inc("rat_gateway_calls_total", "capability-invoke decisions by outcome",
			map[string]string{"capability": capability, "outcome": outcome})
	}
	if plane.Control != nil {
		ctrl := plane.Control
		reg.RegisterGaugeFunc("rat_plugin_up", "1 if the plugin is Healthy, else 0", func() []metrics.Sample {
			resp, lerr := ctrl.ListPlugins(context.Background(), &corev1.ListPluginsRequest{})
			if lerr != nil {
				return nil
			}
			out := make([]metrics.Sample, 0, len(resp.GetPlugins()))
			for _, p := range resp.GetPlugins() {
				up := 0.0
				if p.GetState() == "Healthy" {
					up = 1
				}
				out = append(out, metrics.Sample{Labels: map[string]string{"plugin": p.GetName(), "kind": p.GetKind()}, Value: up})
			}
			return out
		})
	}
	stopMetrics := serveMetrics(os.Getenv("RAT_METRICS_ADDR"), reg)
	defer stopMetrics()

	// Publish this daemon's pid + global registry entry (slice 2c) so `rat down`/`ls`/
	// `status` can find it; retract both on drain. No-op when the plane has no project
	// (.rat/) context — a raw `rat serve --plane …`.
	registerDaemon(pl)
	defer deregisterDaemon(pl)

	// GracefulStop in drain() closes every server. When control is plain TCP (no separate
	// callback door), plugins share the operator listener — an unauthenticated dev posture,
	// flagged at assemble time; the secure default (unix-socket control + 0.0.0.0 callback)
	// authenticates the plugin door.
	serveErr := make(chan error, len(servers))
	go func() { serveErr <- ctlSrv.Serve(ctlLis) }()
	cbDesc := "(same as control — plugins UNAUTHENTICATED; use a unix-socket control for the authenticated plugin door)"
	if cbSrv != nil {
		go func() { serveErr <- cbSrv.Serve(cbLis) }()
		cbDesc = cbLis.Addr().String() + " (token-authenticated)"
	}
	log.Printf("gateway serving — control %s · plugin-callbacks %s — %d plugin(s) up; Ctrl-C / SIGTERM to drain",
		ctlLis.Addr(), cbDesc, len(pl.Specs))

	select {
	case <-ctx.Done():
		log.Print("signal received — draining")
		drain(plane, servers...)
		return nil
	case err := <-serveErr:
		drain(plane, servers...)
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
	Control  corev1.ControlServiceServer // the live admin API (ADR-027); nil in attach mode
	shutdown func(context.Context)
}

func (rp *runningPlane) Shutdown(ctx context.Context) { rp.shutdown(ctx) }

// drain stops every gateway server gracefully (in-flight calls finish, no new ones
// accepted), then tears the plane down (stop the loop, terminate instances, close provider
// conns). The operator + plugin doors are separate gRPC servers (C2), so both are stopped.
func drain(plane *runningPlane, srvs ...*grpc.Server) {
	for _, srv := range srvs {
		srv.GracefulStop()
	}
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
	tokens := map[string]string{} // plugin name -> per-launch bearer token (C2), registered once the gateway exists
	for _, s := range pl.Specs {
		manifests = append(manifests, s.Manifest)
		if s.Launch != nil {
			name := s.Manifest.Metadata.Name
			// Inject the topology-dependent gateway-callback env (RAT_GATEWAY) + the caller
			// identity (RAT_PLUGIN_NAME) + the per-launch bearer token (RAT_PLUGIN_TOKEN) the
			// SAME way the live RegisterPlugin path does.
			tok := newPluginToken()
			injectLaunchEnv(s.Launch, name, gwAddr, tok, s.Manifest.PublishPorts())
			tokens[name] = tok
			desired = append(desired, reconciler.Desired{Name: name, Launch: s.Launch})
		}
	}
	log.Printf("plugins dial the gateway back at %s (injected RAT_GATEWAY)", gwAddr)
	logUnsatisfied(manifests) // poetry-style: warn about any `requires` no plugin provides
	reg, err := registry.New(manifests)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	gw := gateway.New(reg, nil, auditor, routableDescriptors()...)
	// C2: authenticate the plugin door. Each launched plugin presents its token; the gateway
	// derives caller_plugin from it, never from the wire envelope (closes identity forgery).
	for name, tok := range tokens {
		gw.SetPluginToken(tok, name)
	}
	gw.RequirePluginAuth(true)
	rewire := newGatewayRewire(gw)
	rec := reconciler.New(rt, desired, reconciler.Config{
		BaseBackoff:      500 * time.Millisecond,
		MaxBackoff:       10 * time.Second,
		CrashLoopCap:     6,
		ReadinessTimeout: pl.HealthTimeout,
		Rewire:           rewire,
	})
	// Leader election backend (gap #1 / ADR-043): in-memory for solo (default), or a SHARED
	// state-backend over state/v1 CAS when RAT_LEASE_STATE_ADDR is set — the latter is what
	// makes multiple `rat serve` replicas elect exactly one leader (real HA).
	leaseBackend, closeLease, err := newLeaseBackend()
	if err != nil {
		return nil, err
	}
	loopCtx, cancelLoop := context.WithCancel(context.Background())
	loop := &reconciler.Loop{
		Elector:    lease.NewElector(leaseCandidateID(pl.Instance), leaseBackend, 10*time.Second),
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
				closeLease()
				return nil, fmt.Errorf("plugin %q never became healthy within %s", d.Name, pl.HealthTimeout)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	shutdown := func(ctx context.Context) {
		cancelLoop()      // stop the reconcile loop (so no pass races the teardown)
		rec.Shutdown(ctx) // terminate launched instances
		rewire.Close()    // close the gateway provider conns
		closeLease()      // close the shared lease-backend conn (no-op for in-memory)
	}
	// The live control plane (ADR-027): drives the mutable registry + reconciler so a
	// client can register/deregister a plugin against this running daemon — no restart.
	control := &controlService{reg: reg, rec: rec, gw: gw, gwAddr: gwAddr, readyTO: pl.HealthTimeout}
	return &runningPlane{Gateway: gw, Control: control, shutdown: shutdown}, nil
}

// isLoopbackBind reports whether a TCP listen address binds only the loopback interface — so a
// launched container can't reach it via host.containers.internal (Gap 6).
func isLoopbackBind(addr string) bool {
	return strings.HasPrefix(addr, "127.0.0.1") || strings.HasPrefix(addr, "localhost") || strings.HasPrefix(addr, "[::1]")
}

// isContainerRuntime reports whether the deployment runtime launches plugins in containers (so the
// host/container boundary applies to gateway callbacks).
func isContainerRuntime(rt string) bool {
	return rt == "podman" || rt == "docker"
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
func newRuntime(name, instance, dataRoot string) (deploymentruntimev1.DeploymentRuntimeServiceServer, error) {
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
		var p *deploymentruntime.Podman
		if net := os.Getenv("RAT_PODMAN_NETWORK"); net != "" {
			p = deploymentruntime.NewPodmanInstanced(net, instance)
		} else {
			p = deploymentruntime.NewPodman()
		}
		p.DataRoot = dataRoot // durable per-plugin /data mount (ADR-031); "" = ephemeral
		return p, nil
	default:
		return nil, fmt.Errorf("unknown runtime %q", name)
	}
}
