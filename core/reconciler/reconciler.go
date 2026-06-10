// Package reconciler is the core's convergence loop (the 5th of the six things,
// overview.md §reconciliation): it reads a desired set of plugin instances, observes
// actual state via the deployment-runtime, and nudges actual toward desired — launch
// the missing, restart the crashed. It is level-triggered (each pass re-observes;
// events are only hints), the K8s controller pattern.
//
// sre#4 (reviews/03 §incident-runbooks → reviews/09 Phase-1 exit gate) is baked in:
// restarts of a failing plugin use EXPONENTIAL BACKOFF + JITTER with a CRASH-LOOP CAP
// (→ Degraded), so a crash-looping plugin can't hammer the deployment-runtime — the K8s
// CrashLoopBackOff lesson. Leadership (one active reconciler) is the lease package's
// job; Run ties them together.
//
// CONCURRENCY (sre AV-3, gap #4). Two failure modes the spike's single pass-wide mutex
// caused are fixed here: a hung runtime RPC must not (1) PIN the loop forever, nor (2)
// BLIND the observability/control read path. So:
//   - every runtime RPC is DEADLINE-BOUNDED (RPCTimeout) — a wedged Healthcheck/Launch/
//     Terminate is cut, and the pass moves on.
//   - Status/Endpoint read a separate published SNAPSHOT (snapMu), never the reconcile
//     mutex, so a pass blocked in an RPC can't stall a status read.
//   - the desired set has its own lock (desMu), so DesiredNames/AddDesired don't queue
//     behind a pass either.
// The pass still serializes plugins (the reconcile mutex is held across the pass to keep
// RemoveDesired from racing a launch) — but bounded by RPCTimeout, not unboundedly. Fully
// parallel per-plugin reconcile is a separate, later step.
package reconciler

import (
	"context"
	"fmt"
	"sync"
	"time"

	deploymentruntimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/deploymentruntime/v1"
)

// defaultRPCTimeout bounds a single runtime RPC when Config.RPCTimeout is unset.
const defaultRPCTimeout = 5 * time.Second

// State is a desired plugin's reconcile state.
type State int

const (
	// Pending: not yet healthy — either awaiting readiness after a launch, or backing
	// off before the next retry.
	Pending State = iota
	// Healthy: launched and its endpoint is accepting.
	Healthy
	// Degraded: hit the crash-loop cap; the reconciler stops retrying it (no hammering)
	// until it is reset, so one bad plugin can't pin the loop.
	Degraded
)

func (s State) String() string {
	switch s {
	case Healthy:
		return "Healthy"
	case Degraded:
		return "Degraded"
	default:
		return "Pending"
	}
}

// Desired is one plugin the reconciler keeps converged.
type Desired struct {
	Name   string
	Launch *deploymentruntimev1.LaunchSpec
}

// Rewire is notified to (re)bind or drop a plugin's routable connection as its health
// changes: Bind when it becomes Healthy at an endpoint (initial launch OR a crash-relaunch
// on a NEW endpoint), Unbind when a Healthy plugin is lost. The daemon wires this to the
// gateway (dial + gateway.SetProvider / RemoveProvider) — keeping the reconciler decoupled
// from the gateway, the seam that makes a relaunched plugin self-heal routing (ADR-022).
type Rewire interface {
	Bind(name, endpoint string)
	Unbind(name string)
}

// Config tunes the convergence + the sre#4 crash-loop discipline.
type Config struct {
	BaseBackoff      time.Duration                     // retry interval for the 1st failure
	MaxBackoff       time.Duration                     // cap on the per-retry interval
	CrashLoopCap     int                               // consecutive failures → Degraded
	ReadinessTimeout time.Duration                     // a launched-but-not-ready plugin fails after this
	RPCTimeout       time.Duration                     // per-call deadline on each runtime RPC (0 == defaultRPCTimeout) — AV-3
	Jitter           func(time.Duration) time.Duration // extra wait added to each backoff (anti-lockstep); nil == none
	Clock            func() time.Time                  // injectable; nil == time.Now
	Rewire           Rewire                            // optional: (re)bind/unbind routable conns on health change
}

type pluginStatus struct {
	state       State
	instanceID  string // "" == nothing launched
	endpoint    string
	attempts    int       // consecutive failures (resets when Healthy)
	nextRetryAt time.Time // earliest next (re)launch
	launchedAt  time.Time // for the readiness timeout
}

// statusView is the read-only projection Status/Endpoint serve from the snapshot — so a
// status read never touches the reconcile mutex (AV-3).
type statusView struct {
	state       State
	attempts    int
	nextRetryAt time.Time
	endpoint    string
}

// Reconciler converges a desired plugin set through a deployment-runtime.
type Reconciler struct {
	runtime deploymentruntimev1.DeploymentRuntimeServiceServer
	cfg     Config

	desMu   sync.RWMutex // guards desired (separate from the reconcile pass — AV-3)
	desired []Desired

	mu      sync.Mutex // guards plugins; held across a pass, but every RPC under it is deadline-bounded
	plugins map[string]*pluginStatus

	snapMu sync.RWMutex          // guards snap; the Status/Endpoint read path, never held across an RPC
	snap   map[string]statusView // published observable status
}

// New builds a reconciler. The desired set seeds the loop; AddDesired/RemoveDesired
// mutate it at runtime (the live ControlService, ADR-027).
func New(runtime deploymentruntimev1.DeploymentRuntimeServiceServer, desired []Desired, cfg Config) *Reconciler {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Jitter == nil {
		cfg.Jitter = func(time.Duration) time.Duration { return 0 }
	}
	return &Reconciler{
		runtime: runtime,
		cfg:     cfg,
		desired: desired,
		plugins: map[string]*pluginStatus{},
		snap:    map[string]statusView{},
	}
}

// AddDesired adds a plugin to the desired set of a RUNNING reconciler (the live
// ControlService, ADR-027). The next reconcile pass launches + wires it through the
// UNCHANGED convergence path (Pending → launch → Healthy → Rewire.Bind). Rejects a
// duplicate name. Concurrency-safe; does not block behind an in-flight pass.
func (r *Reconciler) AddDesired(d Desired) error {
	r.desMu.Lock()
	defer r.desMu.Unlock()
	for _, x := range r.desired {
		if x.Name == d.Name {
			return fmt.Errorf("plugin %q is already in the desired set", d.Name)
		}
	}
	r.desired = append(r.desired, d)
	return nil
}

// RemoveDesired drops a plugin from the desired set and tears it down: terminate its
// instance, unbind its routing (Rewire.Unbind), and forget its status. A no-op if absent.
// Concurrency-safe.
func (r *Reconciler) RemoveDesired(ctx context.Context, name string) {
	r.desMu.Lock()
	kept := r.desired[:0]
	for _, x := range r.desired {
		if x.Name != name {
			kept = append(kept, x)
		}
	}
	r.desired = kept
	r.desMu.Unlock()

	// Remove the observed status under the reconcile mutex (mutually exclusive with a pass,
	// so it can't race a launch), capturing the instance to terminate OUTSIDE the lock.
	r.mu.Lock()
	ps := r.plugins[name]
	var instanceID string
	if ps != nil {
		instanceID = ps.instanceID
		delete(r.plugins, name)
		r.publishLocked(name)
	}
	r.mu.Unlock()
	if ps != nil {
		r.unbind(name)
		r.terminate(ctx, instanceID)
	}
}

// DesiredNames returns the names currently in the desired set (for ListPlugins). Served
// from the desired lock, so it does not block behind a reconcile pass.
func (r *Reconciler) DesiredNames() []string {
	r.desMu.RLock()
	defer r.desMu.RUnlock()
	out := make([]string, 0, len(r.desired))
	for _, d := range r.desired {
		out = append(out, d.Name)
	}
	return out
}

// Reconcile runs ONE convergence pass over every desired plugin at now. The leader calls it
// on each tick (see Run). One observe-or-act step per plugin per pass.
func (r *Reconciler) Reconcile(ctx context.Context, now time.Time) {
	r.desMu.RLock()
	desired := append([]Desired(nil), r.desired...)
	r.desMu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range desired {
		r.reconcileOne(ctx, d, now)
	}
}

// reconcileOne advances one plugin. The caller holds r.mu; the only blocking inside is the
// deadline-bounded runtime RPCs (status/launch/terminate), so the pass is bounded by
// RPCTimeout per plugin rather than an unbounded hung call. It republishes the plugin's
// observable status on the way out.
func (r *Reconciler) reconcileOne(ctx context.Context, d Desired, now time.Time) {
	defer r.publishLocked(d.Name)

	ps := r.plugins[d.Name]
	if ps == nil {
		ps = &pluginStatus{state: Pending}
		r.plugins[d.Name] = ps
	}

	switch ps.state {
	case Degraded:
		return // capped — do not hammer the runtime

	case Healthy:
		if r.status(ctx, ps) != deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY {
			r.unbind(d.Name)     // lost → drop it from routing before relaunch (ADR-022)
			r.fail(ctx, ps, now) // a plugin that was healthy then crashed → back off + restart
		}
		return

	case Pending:
		if ps.instanceID == "" {
			if now.Before(ps.nextRetryAt) {
				return // backing off
			}
			r.launch(ctx, ps, d, now)
			return
		}
		switch r.status(ctx, ps) {
		case deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY:
			ps.state, ps.attempts = Healthy, 0 // success resets the crash-loop counter
			r.bind(d.Name, ps.endpoint)        // (re)wire it into routing — self-heals a relaunch (ADR-022)
		case deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNKNOWN:
			if now.Sub(ps.launchedAt) > r.cfg.ReadinessTimeout {
				r.fail(ctx, ps, now) // never became ready in time
			}
		default: // UNHEALTHY / UNSPECIFIED → crashed
			r.fail(ctx, ps, now)
		}
	}
}

// launch starts a fresh instance. A launch error is itself a failure (a bad image / an
// I9 refusal crash-loops through here too).
func (r *Reconciler) launch(ctx context.Context, ps *pluginStatus, d Desired, now time.Time) {
	rctx, cancel := r.rpcCtx(ctx)
	defer cancel()
	lr, err := r.runtime.Launch(rctx, &deploymentruntimev1.LaunchRequest{PluginId: d.Name, Spec: d.Launch})
	if err != nil {
		r.fail(ctx, ps, now)
		return
	}
	ps.instanceID, ps.endpoint, ps.launchedAt = lr.GetInstanceId(), lr.GetEndpoint(), now
}

// status reads the runtime's health for the current instance (an error, a timeout, or no
// instance is treated as UNHEALTHY — the instance is gone / unreachable). The RPC is
// deadline-bounded so a wedged Healthcheck can't pin the pass (AV-3).
func (r *Reconciler) status(ctx context.Context, ps *pluginStatus) deploymentruntimev1.HealthStatus {
	if ps.instanceID == "" {
		return deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNHEALTHY
	}
	rctx, cancel := r.rpcCtx(ctx)
	defer cancel()
	hc, err := r.runtime.Healthcheck(rctx, &deploymentruntimev1.HealthcheckRequest{InstanceId: ps.instanceID})
	if err != nil {
		return deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNHEALTHY
	}
	return hc.GetStatus()
}

// fail records a failed reconcile: terminate the dead instance, count the failure, and
// either cap out to Degraded or schedule the next retry with exponential backoff+jitter.
func (r *Reconciler) fail(ctx context.Context, ps *pluginStatus, now time.Time) {
	if ps.instanceID != "" {
		r.terminate(ctx, ps.instanceID)
		ps.instanceID, ps.endpoint = "", ""
	}
	ps.attempts++
	if ps.attempts >= r.cfg.CrashLoopCap {
		ps.state = Degraded
		return
	}
	ps.state = Pending
	ps.nextRetryAt = now.Add(r.backoffFor(ps.attempts))
}

// terminate force-removes an instance, deadline-bounded, ignoring the result (best-effort
// cleanup — a hung Terminate must not pin the pass either).
func (r *Reconciler) terminate(ctx context.Context, instanceID string) {
	if instanceID == "" {
		return
	}
	rctx, cancel := r.rpcCtx(ctx)
	defer cancel()
	_, _ = r.runtime.Terminate(rctx, &deploymentruntimev1.TerminateRequest{InstanceId: instanceID})
}

// rpcCtx derives a per-call deadline for a runtime RPC (AV-3).
func (r *Reconciler) rpcCtx(parent context.Context) (context.Context, context.CancelFunc) {
	to := r.cfg.RPCTimeout
	if to <= 0 {
		to = defaultRPCTimeout
	}
	return context.WithTimeout(parent, to)
}

// backoffFor is BaseBackoff * 2^(attempt-1), capped at MaxBackoff, plus jitter.
func (r *Reconciler) backoffFor(attempt int) time.Duration {
	d := r.cfg.BaseBackoff << (attempt - 1)
	if d <= 0 || d > r.cfg.MaxBackoff { // <=0 guards the shift overflowing at high attempt counts
		d = r.cfg.MaxBackoff
	}
	return d + r.cfg.Jitter(d)
}

// bind/unbind notify the Rewire hook (if set) so the gateway can (re)bind or drop the
// plugin's routable connection — the seam that self-heals routing across a relaunch.
func (r *Reconciler) bind(name, endpoint string) {
	if r.cfg.Rewire != nil {
		r.cfg.Rewire.Bind(name, endpoint)
	}
}

func (r *Reconciler) unbind(name string) {
	if r.cfg.Rewire != nil {
		r.cfg.Rewire.Unbind(name)
	}
}

// publishLocked copies a plugin's observable status into the read snapshot (deleting it when
// the plugin is gone). The caller holds r.mu; this briefly takes snapMu — the only lock
// Status/Endpoint touch, so reads never wait on the pass's RPCs. (Lock order r.mu → snapMu.)
func (r *Reconciler) publishLocked(name string) {
	ps := r.plugins[name]
	r.snapMu.Lock()
	if ps == nil {
		delete(r.snap, name)
	} else {
		r.snap[name] = statusView{state: ps.state, attempts: ps.attempts, nextRetryAt: ps.nextRetryAt, endpoint: ps.endpoint}
	}
	r.snapMu.Unlock()
}

// ── observability (tests + the eventual /metrics) ────────────────────────────

// Status reports a plugin's reconcile state, consecutive-failure count, and the time of its
// next scheduled retry — read from the published snapshot, so it never blocks behind a
// reconcile pass stuck in a runtime RPC (AV-3).
func (r *Reconciler) Status(name string) (state State, attempts int, nextRetryAt time.Time) {
	r.snapMu.RLock()
	defer r.snapMu.RUnlock()
	v, ok := r.snap[name]
	if !ok {
		return Pending, 0, time.Time{}
	}
	return v.state, v.attempts, v.nextRetryAt
}

// Endpoint returns the dialable endpoint of a Healthy plugin ("" otherwise) — also from the
// snapshot, off the reconcile mutex.
func (r *Reconciler) Endpoint(name string) string {
	r.snapMu.RLock()
	defer r.snapMu.RUnlock()
	if v, ok := r.snap[name]; ok && v.state == Healthy {
		return v.endpoint
	}
	return ""
}

// Shutdown terminates every instance the reconciler launched. The caller stops the Loop
// first (so no pass races this). Instances are collected under the lock, then terminated
// outside it (each deadline-bounded).
func (r *Reconciler) Shutdown(ctx context.Context) {
	r.mu.Lock()
	ids := make([]string, 0, len(r.plugins))
	for name, ps := range r.plugins {
		if ps.instanceID != "" {
			ids = append(ids, ps.instanceID)
			ps.instanceID = ""
			r.publishLocked(name)
		}
	}
	r.mu.Unlock()
	for _, id := range ids {
		r.terminate(ctx, id)
	}
}
