package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rat-dev/rat/core/manifest"
	"github.com/rat-dev/rat/core/supervisor"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	"gopkg.in/yaml.v3"
)

// plane.yaml is the daemon's only config (ADR-019): the desired plugin set for one
// RAT plane. This file parses + validates it and translates it into the
// supervisor.PluginSpec list the assembly in main.go brings up.
//
//	name: my-project            # instance id (ADR-023); else derived from this file's dir.
//	                            # namespaces podman resources so many rats coexist.
//	addr: unix:./.rat/daemon.sock  # gateway listen address: a per-project UNIX SOCKET
//	                            # (ADR-023, no port war) or a TCP host:port (":0" = auto-port).
//	runtime: local              # local | podman
//	health_timeout: 10s         # per-plugin readiness wait (launch mode)
//	plugins:
//	  - name: rat-state                   # MUST equal manifest metadata.name
//	    manifest: ./manifests/state.plugin.yaml
//	    launch:                           # launch mode: the daemon starts it
//	      image: ./bin/stateplugin        # local: a binary path · podman: an OCI image
//	      isolation: i9                   # the I9 profile (non-root, cap_drop ALL, …)
//	      env: { FOO: bar }               # optional; NEVER secrets
//	  - name: rat-caller                  # a register-only driver: neither launch nor
//	    manifest: ./manifests/caller.plugin.yaml  # endpoint — registered for its `requires`
//	                                      # so C5 can authorize the calls it makes.

const (
	defaultAddr          = "0.0.0.0:7777"
	defaultRuntime       = "local"
	defaultHealthTimeout = 10 * time.Second
)

// rawPlane / rawPlugin / rawLaunch mirror the on-disk YAML before validation.
type rawPlane struct {
	Name          string      `yaml:"name"`
	Addr          string      `yaml:"addr"`
	Runtime       string      `yaml:"runtime"`
	HealthTimeout string      `yaml:"health_timeout"`
	Plugins       []rawPlugin `yaml:"plugins"`
}

type rawPlugin struct {
	Name     string     `yaml:"name"`
	Manifest string     `yaml:"manifest"`
	Launch   *rawLaunch `yaml:"launch"`
	Endpoint string     `yaml:"endpoint"`
}

type rawLaunch struct {
	Image     string            `yaml:"image"`
	Isolation string            `yaml:"isolation"`
	Env       map[string]string `yaml:"env"`
}

// Plane is the validated, ready-to-bring-up plane: the listen address, the chosen
// runtime, the per-plugin readiness wait, and the supervisor specs (launch providers
// + register-only drivers).
type Plane struct {
	Instance      string // per-project instance id (ADR-023) — namespaces runtime resources
	Addr          string
	Runtime       string
	HealthTimeout time.Duration
	Specs         []supervisor.PluginSpec

	// CallbackAddr is the network address a launched DRIVER plugin dials to reach the
	// gateway back (injected as RAT_GATEWAY). serve() fills it once the listeners exist —
	// when the control plane is a unix socket (unreachable by plugins), it is the auto-port
	// TCP companion; when control is already TCP, it is that. Empty until serve() sets it.
	CallbackAddr string

	// RuntimeDir is the project's `.rat/` directory (ADR-023 slice 2c). When set, the daemon
	// writes its pid there and registers itself in the machine-global instance registry so
	// `rat down`/`rat ls`/`rat status` can find it. Empty for a raw `rat serve --plane` (no
	// project context). Set by LoadProject.
	RuntimeDir string
}

// LoadPlane parses + validates the plane file at path. Manifest and (local) image
// paths in the file are resolved relative to the plane file's directory, so a plane
// is relocatable. Phase A supports launch mode (`launch:`) and register-only drivers
// (neither launch nor endpoint); attach mode (`endpoint:`) arrives in Phase C.
func LoadPlane(path string) (*Plane, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plane %s: %w", path, err)
	}
	var rp rawPlane
	if err := yaml.Unmarshal(data, &rp); err != nil {
		return nil, fmt.Errorf("parse plane %s: %w", path, err)
	}
	return planeFromRaw(&rp, path)
}

// planeFromRaw validates a parsed plane (from YAML plane.yaml OR TOML rat.toml — both
// reduce to rawPlane) and builds the ready-to-bring-up Plane. Manifest and (local) image
// paths resolve relative to the file's directory, so a plane/project is relocatable.
func planeFromRaw(rp *rawPlane, path string) (*Plane, error) {
	pl := &Plane{
		// The instance id (ADR-023) namespaces this daemon's runtime resources (podman
		// network + container names) so many rats coexist on one machine. Explicit `name:`
		// wins; else derive from the plane file's directory (a project is a directory).
		Instance:      instanceID(orDefault(rp.Name, filepath.Base(filepath.Dir(absPath(path))))),
		Addr:          orDefault(rp.Addr, defaultAddr),
		Runtime:       orDefault(rp.Runtime, defaultRuntime),
		HealthTimeout: defaultHealthTimeout,
	}
	if rp.HealthTimeout != "" {
		d, err := time.ParseDuration(rp.HealthTimeout)
		if err != nil {
			return nil, fmt.Errorf("plane %s: health_timeout %q: %w", path, rp.HealthTimeout, err)
		}
		pl.HealthTimeout = d
	}
	if pl.Runtime != "local" && pl.Runtime != "podman" {
		return nil, fmt.Errorf("plane %s: runtime %q must be \"local\" or \"podman\"", path, pl.Runtime)
	}
	if len(rp.Plugins) == 0 {
		return nil, fmt.Errorf("plane %s: no plugins declared", path)
	}

	dir := filepath.Dir(path)
	for i, rpl := range rp.Plugins {
		spec, err := pl.specFor(dir, rpl)
		if err != nil {
			return nil, fmt.Errorf("plane %s: plugins[%d] (%q): %w", path, i, rpl.Name, err)
		}
		pl.Specs = append(pl.Specs, spec)
	}
	return pl, nil
}

// specFor validates one plugin entry and builds its supervisor.PluginSpec.
func (pl *Plane) specFor(dir string, rpl rawPlugin) (supervisor.PluginSpec, error) {
	var zero supervisor.PluginSpec
	if rpl.Name == "" {
		return zero, fmt.Errorf("missing name")
	}
	if rpl.Manifest == "" {
		return zero, fmt.Errorf("missing manifest")
	}
	if rpl.Launch != nil && rpl.Endpoint != "" {
		return zero, fmt.Errorf("has both launch: and endpoint: — at most one")
	}

	m, err := manifest.Load(resolve(dir, rpl.Manifest))
	if err != nil {
		return zero, err
	}
	if m.Metadata.Name != rpl.Name {
		return zero, fmt.Errorf("name %q != manifest metadata.name %q", rpl.Name, m.Metadata.Name)
	}

	switch {
	case rpl.Endpoint != "":
		// Attach mode: the daemon dials an already-running plugin (the orchestrator —
		// e.g. compose — started it). supervisor.Attach handles these; no launch.
		return supervisor.PluginSpec{Manifest: m, Endpoint: rpl.Endpoint}, nil

	case rpl.Launch != nil:
		if rpl.Launch.Image == "" {
			return zero, fmt.Errorf("launch.image is required")
		}
		iso, err := isolationProfile(rpl.Launch.Isolation)
		if err != nil {
			return zero, err
		}
		image := rpl.Launch.Image
		// For the local-process runtime the image is a filesystem path the daemon
		// execs; resolve a relative one against the plane dir so the plane is
		// relocatable. For podman the image is an OCI reference — left verbatim.
		if pl.Runtime == "local" {
			image = resolve(dir, image)
		}
		return supervisor.PluginSpec{
			Manifest: m,
			Launch:   &deploymentruntimev1.LaunchSpec{Image: image, Isolation: iso, Env: rpl.Launch.Env},
		}, nil

	default:
		// Register-only driver: no launch, no endpoint. Registered so the gateway
		// knows its `requires` for C5, but not itself launched as a provider
		// (supervisor PluginSpec.Launch == nil — see supervisor.BringUp).
		return supervisor.PluginSpec{Manifest: m}, nil
	}
}

// isolationProfile maps the plane file's isolation shorthand to a LaunchSpec
// profile. "i9" (or empty, the default) is the full I9 profile every runtime's
// trust gate (checkI9Minimum) requires: non-root + cap-drop-all + no-new-privs,
// plus read-only-rootfs + metadata-egress-block + a default seccomp profile that
// the podman runtime actually enforces (the local-process runtime honors the
// process-level subset). It is the only profile name v1 accepts.
func isolationProfile(name string) (*deploymentruntimev1.IsolationProfile, error) {
	switch name {
	case "", "i9":
		return &deploymentruntimev1.IsolationProfile{
			RunAsNonRoot:        true,
			DropAllCapabilities: true,
			NoNewPrivileges:     true,
			ReadOnlyRootFs:      true,
			BlockMetadataEgress: true,
			SeccompProfile:      "RuntimeDefault",
		}, nil
	default:
		return nil, fmt.Errorf("unknown isolation profile %q (only \"i9\" is supported in v1)", name)
	}
}

func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// instanceID sanitizes a name into a podman-legal, lowercase resource id (used as a
// network suffix + container-name prefix). Non-alnum bytes → '-'; empty → "rat".
func instanceID(name string) string {
	b := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			b = append(b, c)
		case c >= 'A' && c <= 'Z':
			b = append(b, c+('a'-'A')) // lowercase
		default:
			b = append(b, '-')
		}
	}
	s := strings.Trim(string(b), "-")
	if s == "" {
		return "rat"
	}
	return s
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
