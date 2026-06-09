// Package manifest loads and represents RAT plugin manifests — the frozen
// plugin.v1.json shape (ADR-011), as hand-written Go structs over the YAML.
//
// This is the spike's deliberate small dup of the JSON Schema, pending
// manifest-from-schema codegen in the full build (ADR-014, accepted negative #2).
// It carries only the subset the C5 enforcement spine needs: kind, identity, and
// the provides/requires capability lists that the registry's authorization
// decision is DERIVED from (not a hardcoded allowlist).
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

// capURIRe is the rat://<axis>/v<major>/<capability> grammar (plugin.v1.json:174-177).
var capURIRe = regexp.MustCompile(`^rat://[a-z][a-z0-9-]*/v[1-9][0-9]*/[a-z][a-z0-9-]*$`)

// ValidCapabilityURI reports whether s matches the capability URI grammar.
func ValidCapabilityURI(s string) bool { return capURIRe.MatchString(s) }

// CapabilityRef is one entry in a manifest's provides/requires/suggests list.
type CapabilityRef struct {
	Capability string `yaml:"capability"`
}

// Metadata is the manifest metadata block (subset used by the spike).
type Metadata struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// ResourceQuantities mirrors plugin.v1.json $defs/resourceQuantities (cpu/memory asks).
type ResourceQuantities struct {
	CPU    string `yaml:"cpu"`
	Memory string `yaml:"memory"`
}

// Resources is the C4 resource asks/limits block (plugin.v1.json requires `requests`).
type Resources struct {
	Requests ResourceQuantities `yaml:"requests"`
	Limits   ResourceQuantities `yaml:"limits"`
}

// Port is a browser/HTTP port a plugin publishes for out-of-band (non-gateway) access — e.g. a
// ui plugin's web port. ADR-040 (Gap 9): the deployment-runtime publishes these to the host so a
// UI plugin is launched + reconciled like any other plugin instead of run as a sidecar.
type Port struct {
	Name          string `yaml:"name"`
	ContainerPort int    `yaml:"container_port"`
}

// CommandArg maps one CLI token (a positional or a --flag) onto a field of the capability's
// request message (ADR-041).
type CommandArg struct {
	Name       string `yaml:"name" json:"name"`
	Field      string `yaml:"field" json:"field"`
	Positional bool   `yaml:"positional" json:"positional"`
	Required   bool   `yaml:"required" json:"required"`
	Default    string `yaml:"default" json:"default"`
}

// Command is a CLI command a plugin contributes — the `rat` client surfaces it as `rat <name>`
// and dispatches it onto `capability` through the gateway (ADR-041).
type Command struct {
	Name       string       `yaml:"name" json:"name"`
	Capability string       `yaml:"capability" json:"capability"`
	Help       string       `yaml:"help" json:"help"`
	Args       []CommandArg `yaml:"args" json:"args"`
}

// Contributes is what a plugin contributes into shared surfaces — UI slots (ADR-024/025) and CLI
// commands (ADR-041).
type Contributes struct {
	Commands []Command `yaml:"commands"`
}

// Manifest is a parsed plugin manifest (subset of plugin.v1.json the spike needs).
type Manifest struct {
	APIVersion     string          `yaml:"api_version"`
	Kind           string          `yaml:"kind"`
	Metadata       Metadata        `yaml:"metadata"`
	CompatibleCore []string        `yaml:"compatible_core"`
	Provides       []CapabilityRef `yaml:"provides"`
	Requires       []CapabilityRef `yaml:"requires"`
	Suggests       []CapabilityRef `yaml:"suggests"`
	Resources      Resources       `yaml:"resources"`
	Ports          []Port          `yaml:"ports"`
	Contributes    Contributes     `yaml:"contributes"`

	// Path is the file the manifest was loaded from (diagnostics only; not on the wire).
	Path string `yaml:"-"`
}

// PublishPorts returns the declared container ports as strings (for the RAT_PUBLISH_PORTS launch
// directive the deployment-runtime reads, ADR-040).
func (m *Manifest) PublishPorts() []string {
	out := make([]string, 0, len(m.Ports))
	for _, p := range m.Ports {
		if p.ContainerPort > 0 {
			out = append(out, fmt.Sprintf("%d", p.ContainerPort))
		}
	}
	return out
}

// HasResources reports whether the C4 resources.requests block declares at least one quantity.
// (Parsing-presence only — the wire/runtime Validate() stays lenient so minimal runtime manifests
// still load; the authoring gate `rat plugin check`/`pack` is where this is required.)
func (m *Manifest) HasResources() bool {
	return m.Resources.Requests.CPU != "" || m.Resources.Requests.Memory != ""
}

// ProvidesCaps returns the capability URIs this plugin provides.
func (m *Manifest) ProvidesCaps() []string { return uris(m.Provides) }

// RequiresCaps returns the capability URIs this plugin requires.
func (m *Manifest) RequiresCaps() []string { return uris(m.Requires) }

func uris(refs []CapabilityRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Capability)
	}
	return out
}

// Load parses and validates a single manifest file.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m.Path = path
	return m, nil
}

// Parse parses and validates a manifest from raw YAML bytes (the wire form the live
// ControlService.RegisterPlugin carries, ADR-027 — same validation as Load, no file).
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	return &m, nil
}

// LoadDir loads every *.plugin.yaml in dir, sorted by filename for determinism.
func LoadDir(dir string) ([]*Manifest, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.plugin.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	out := make([]*Manifest, 0, len(matches))
	for _, p := range matches {
		m, err := Load(p)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Validate checks the structural invariants the spike relies on. It is a subset
// of the JSON Schema gate (scripts/validate-manifests.py) — enough for the
// registry to trust the provides/requires lists it indexes.
func (m *Manifest) Validate() error {
	if m.Kind == "" {
		return fmt.Errorf("missing kind")
	}
	if m.Metadata.Name == "" {
		return fmt.Errorf("missing metadata.name")
	}
	for _, c := range m.Provides {
		if !ValidCapabilityURI(c.Capability) {
			return fmt.Errorf("malformed `provides` capability URI %q", c.Capability)
		}
	}
	for _, c := range m.Requires {
		if !ValidCapabilityURI(c.Capability) {
			return fmt.Errorf("malformed `requires` capability URI %q", c.Capability)
		}
	}
	return nil
}
