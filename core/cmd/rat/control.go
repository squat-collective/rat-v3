package main

// control.go — the daemon's ControlService (ADR-027): the admin face that mutates the
// RUNNING plane (register/deregister a plugin, no restart) by driving the now-mutable
// registry + reconciler. It is served on the control listener ALONGSIDE the capability-
// invoke gateway. Operator authz is the control listener's reachability (the per-project
// unix socket = filesystem perms, ADR-023); these handlers do not run C5 — control is an
// operator action, not a plugin capability call.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"strings"
	"time"

	"github.com/squat-collective/rat-v3/core/gateway"
	"github.com/squat-collective/rat-v3/core/manifest"
	"github.com/squat-collective/rat-v3/core/reconciler"
	"github.com/squat-collective/rat-v3/core/registry"
	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	deploymentruntimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/deploymentruntime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// controlService implements corev1.ControlServiceServer. It holds the live registry +
// reconciler the running daemon assembled (launch mode only — rat is the launcher), plus
// the gateway-callback address it injects into a newly-launched plugin's env (the same
// RAT_GATEWAY/RAT_PLUGIN_NAME launchPlane injects for the initial set).
type controlService struct {
	corev1.UnimplementedControlServiceServer
	reg     *registry.Registry
	rec     *reconciler.Reconciler
	gw      *gateway.Gateway // the live gateway — to register/drop the plugin's C2 bearer token
	gwAddr  string           // address launched plugins dial the gateway back on
	readyTO time.Duration    // how long to wait for a registered plugin to become Healthy
}

// RegisterPlugin adds a plugin to the running plane: validate its manifest, register it
// (C5 now knows it), add it to the reconciler's desired set (launch + wire), and wait —
// bounded — for Healthy. On a launch/readiness failure it rolls the change back so a failed
// live-add leaves no partial state. Idempotent: re-registering a present plugin bounces it
// to refresh the spec.
func (c *controlService) RegisterPlugin(ctx context.Context, req *corev1.RegisterPluginRequest) (*corev1.RegisterPluginResponse, error) {
	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name required")
	}
	m, err := manifest.Parse(req.GetManifestYaml())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "manifest: %v", err)
	}
	if m.Metadata.Name != name {
		return nil, status.Errorf(codes.InvalidArgument, "manifest name %q != request name %q", m.Metadata.Name, name)
	}

	// Idempotent refresh: a present plugin is removed first, so a re-register applies the new
	// spec (a bounce). A clean add otherwise.
	if c.reg.Plugin(name) != nil {
		c.rec.RemoveDesired(ctx, name)
		c.reg.Deregister(name)
	}
	if err := c.reg.Register(m); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "register: %v", err)
	}

	// Build the launch spec. No image → a register-only driver (registered, not launched).
	ls := req.GetLaunch()
	if ls == nil || ls.GetImage() == "" {
		log.Printf("control: registered driver %q (no launch)", name)
		return &corev1.RegisterPluginResponse{Name: name, State: "registered"}, nil
	}
	profile, err := isolationProfile(ls.GetIsolation())
	if err != nil {
		c.reg.Deregister(name) // roll back the registry mutation
		return nil, status.Errorf(codes.InvalidArgument, "isolation: %v", err)
	}
	spec := &deploymentruntimev1.LaunchSpec{Image: ls.GetImage(), Isolation: profile, Env: ls.GetEnv()}
	// C2: mint + register this plugin's bearer token so the gateway authenticates it on the
	// plugin door identically to a booted plugin. Dropped on every rollback path below.
	tok := newPluginToken()
	injectLaunchEnv(spec, name, c.gwAddr, tok, m.PublishPorts())
	if c.gw != nil {
		c.gw.SetPluginToken(tok, name)
	}

	if err := c.rec.AddDesired(reconciler.Desired{Name: name, Launch: spec}); err != nil {
		c.dropToken(name)
		c.reg.Deregister(name)
		return nil, status.Errorf(codes.FailedPrecondition, "add desired: %v", err)
	}

	// Wait (bounded) for the reconciler to launch + bind it. Roll back on failure.
	deadline := time.Now().Add(c.readyTO + 5*time.Second)
	for {
		st, _, _ := c.rec.Status(name)
		if st == reconciler.Healthy {
			ep := c.rec.Endpoint(name)
			log.Printf("control: registered + launched %q (Healthy at %s)", name, ep)
			return &corev1.RegisterPluginResponse{Name: name, State: st.String(), Endpoint: ep}, nil
		}
		if st == reconciler.Degraded || time.Now().After(deadline) {
			c.rec.RemoveDesired(ctx, name) // rollback: terminate + unbind
			c.dropToken(name)
			c.reg.Deregister(name)
			return nil, status.Errorf(codes.DeadlineExceeded,
				"plugin %q did not become healthy (state=%s) — rolled back", name, st)
		}
		select {
		case <-ctx.Done():
			c.rec.RemoveDesired(ctx, name)
			c.dropToken(name)
			c.reg.Deregister(name)
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// DeregisterPlugin removes a plugin from the running plane: drop it from the desired set
// (terminate + unbind), then deregister it. A no-op (was_present=false) if absent.
func (c *controlService) DeregisterPlugin(ctx context.Context, req *corev1.DeregisterPluginRequest) (*corev1.DeregisterPluginResponse, error) {
	name := req.GetName()
	present := c.reg.Plugin(name) != nil
	c.rec.RemoveDesired(ctx, name)
	c.dropToken(name)
	c.reg.Deregister(name)
	if present {
		log.Printf("control: deregistered %q", name)
	}
	return &corev1.DeregisterPluginResponse{Name: name, WasPresent: present}, nil
}

// dropToken revokes a plugin's C2 bearer token (a no-op when the gateway isn't wired).
func (c *controlService) dropToken(name string) {
	if c.gw != nil {
		c.gw.RemovePluginToken(name)
	}
}

// ListPlugins reports the live plane: every registered plugin, joined with its reconcile
// state + endpoint (a register-only driver has no launch, so it reads "registered").
func (c *controlService) ListPlugins(_ context.Context, _ *corev1.ListPluginsRequest) (*corev1.ListPluginsResponse, error) {
	launched := map[string]bool{}
	for _, n := range c.rec.DesiredNames() {
		launched[n] = true
	}
	out := make([]*corev1.PluginStatus, 0)
	for _, m := range c.reg.All() {
		name := m.Metadata.Name
		ps := &corev1.PluginStatus{Name: name, Kind: m.Kind, Provides: m.ProvidesCaps()}
		if launched[name] {
			st, _, _ := c.rec.Status(name)
			ps.State = st.String()
			ps.Endpoint = c.rec.Endpoint(name)
		} else {
			ps.State = "registered"
		}
		out = append(out, ps)
	}
	return &corev1.ListPluginsResponse{Plugins: out}, nil
}

// injectLaunchEnv supplies the topology-dependent env vars a launched plugin needs to call
// the gateway BACK — RAT_GATEWAY (the callback address), RAT_PLUGIN_NAME (its caller identity),
// and RAT_PLUGIN_TOKEN (its C2 bearer token for the authenticated plugin door) — as DEFAULTS
// (an explicit plane/request env still wins). Shared by launchPlane (the initial set) and the
// live RegisterPlugin path, so a live-added plugin is wired identically to a booted one.
func injectLaunchEnv(spec *deploymentruntimev1.LaunchSpec, name, gwAddr, token string, publishPorts []string) {
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	if _, set := spec.Env["RAT_GATEWAY"]; !set {
		spec.Env["RAT_GATEWAY"] = gwAddr
	}
	if _, set := spec.Env["RAT_PLUGIN_NAME"]; !set {
		spec.Env["RAT_PLUGIN_NAME"] = name
	}
	if _, set := spec.Env["RAT_PLUGIN_TOKEN"]; !set && token != "" {
		spec.Env["RAT_PLUGIN_TOKEN"] = token
	}
	// ADR-040 (Gap 9): browser/HTTP ports the plugin declares in its manifest `ports`, passed to
	// the deployment-runtime as a launch directive so it publishes them to the host.
	if len(publishPorts) > 0 {
		if _, set := spec.Env["RAT_PUBLISH_PORTS"]; !set {
			spec.Env["RAT_PUBLISH_PORTS"] = strings.Join(publishPorts, ",")
		}
	}
}

// newPluginToken mints a 128-bit random bearer token (hex) for a launched plugin's C2
// identity on the gateway's plugin door. Per-launch + secret: a relaunch re-mints, and only
// the launched plugin and the gateway ever hold it.
func newPluginToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
