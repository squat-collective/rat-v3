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
package reconciler

import (
	"context"
	"sync"
	"time"

	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
)

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

// Reconciler converges a desired plugin set through a deployment-runtime.
type Reconciler struct {
	runtime deploymentruntimev1.DeploymentRuntimeServiceServer
	cfg     Config
	desired []Desired

	mu      sync.Mutex
	plugins map[string]*pluginStatus
}

// New builds a reconciler. The desired set is fixed for the spike (the full core reads
// it from the state-backend).
func New(runtime deploymentruntimev1.DeploymentRuntimeServiceServer, desired []Desired, cfg Config) *Reconciler {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Jitter == nil {
		cfg.Jitter = func(time.Duration) time.Duration { return 0 }
	}
	return &Reconciler{runtime: runtime, cfg: cfg, desired: desired, plugins: map[string]*pluginStatus{}}
}

// Reconcile runs ONE convergence pass over every desired plugin at now. The leader
// calls it on each tick (see Run). One observe-or-act step per plugin per pass.
func (r *Reconciler) Reconcile(ctx context.Context, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.desired {
		r.reconcileOne(ctx, d, now)
	}
}

func (r *Reconciler) reconcileOne(ctx context.Context, d Desired, now time.Time) {
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
	lr, err := r.runtime.Launch(ctx, &deploymentruntimev1.LaunchRequest{PluginId: d.Name, Spec: d.Launch})
	if err != nil {
		r.fail(ctx, ps, now)
		return
	}
	ps.instanceID, ps.endpoint, ps.launchedAt = lr.GetInstanceId(), lr.GetEndpoint(), now
}

// status reads the runtime's health for the current instance (an error or no instance
// is treated as UNHEALTHY — the instance is gone).
func (r *Reconciler) status(ctx context.Context, ps *pluginStatus) deploymentruntimev1.HealthStatus {
	if ps.instanceID == "" {
		return deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNHEALTHY
	}
	hc, err := r.runtime.Healthcheck(ctx, &deploymentruntimev1.HealthcheckRequest{InstanceId: ps.instanceID})
	if err != nil {
		return deploymentruntimev1.HealthStatus_HEALTH_STATUS_UNHEALTHY
	}
	return hc.GetStatus()
}

// fail records a failed reconcile: terminate the dead instance, count the failure, and
// either cap out to Degraded or schedule the next retry with exponential backoff+jitter.
func (r *Reconciler) fail(ctx context.Context, ps *pluginStatus, now time.Time) {
	if ps.instanceID != "" {
		_, _ = r.runtime.Terminate(ctx, &deploymentruntimev1.TerminateRequest{InstanceId: ps.instanceID})
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

// ── observability (tests + the eventual /metrics) ────────────────────────────

// Status reports a plugin's reconcile state, consecutive-failure count, and the time
// of its next scheduled retry.
func (r *Reconciler) Status(name string) (state State, attempts int, nextRetryAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ps := r.plugins[name]
	if ps == nil {
		return Pending, 0, time.Time{}
	}
	return ps.state, ps.attempts, ps.nextRetryAt
}

// Endpoint returns the dialable endpoint of a Healthy plugin ("" otherwise).
func (r *Reconciler) Endpoint(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ps := r.plugins[name]; ps != nil && ps.state == Healthy {
		return ps.endpoint
	}
	return ""
}

// Shutdown terminates every instance the reconciler launched. The caller stops the
// Loop first (so no pass races this).
func (r *Reconciler) Shutdown(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ps := range r.plugins {
		if ps.instanceID != "" {
			_, _ = r.runtime.Terminate(ctx, &deploymentruntimev1.TerminateRequest{InstanceId: ps.instanceID})
			ps.instanceID = ""
		}
	}
}
