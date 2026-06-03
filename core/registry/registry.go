// Package registry is the spike core's manifest-driven plugin registry (ADR-014).
//
// It loads plugin manifests and answers two questions the capability-invoke
// gateway asks on every call: "who provides capability X?" and "is caller P
// authorized to invoke X?". Crucially, the authorization decision is DERIVED from
// the plugins' own declared manifests — caller.requires ∧ provider.provides — not
// a hardcoded allowlist (the self-assertion the throwaway test gateways faked).
// Making that decision real, against the frozen manifests, is the whole point of
// the C5 spike (ADR-013 / ADR-014).
package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/rat-dev/rat/core/manifest"
)

// Decision is the result of a C5 capability-authorization check. It is the value
// the gateway turns into either a proxied call or a PERMISSION_DENIED — and the
// value it audits (C4), allow or deny.
type Decision struct {
	Allowed  bool
	Provider string // the plugin that provides the capability ("" if none)
	Reason   string // human-readable rationale (always set; the audit/deny message)
}

// Registry indexes loaded manifests by plugin name and by provided capability. It is
// concurrency-safe: the live ControlService mutates it (Register/Deregister, ADR-027)
// while the gateway reads it (Authorize/ProviderOf) on every call.
type Registry struct {
	mu         sync.RWMutex
	byName     map[string]*manifest.Manifest
	providerOf map[string]string // capability URI -> providing plugin name
}

// New builds a registry from a set of manifests. It rejects duplicate plugin
// names and duplicate capability providers: the spike has no provider-selection
// policy, so two plugins claiming the same capability is an ambiguity the core
// must refuse rather than resolve arbitrarily.
func New(manifests []*manifest.Manifest) (*Registry, error) {
	r := &Registry{
		byName:     make(map[string]*manifest.Manifest, len(manifests)),
		providerOf: make(map[string]string),
	}
	for _, m := range manifests {
		if err := r.insert(m); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// insert adds one manifest, enforcing the no-dup-name / no-dup-provider invariants
// ATOMICALLY (all checks before any mutation, so a rejected insert leaves no partial
// provider entries). The caller holds the write lock — or owns r exclusively, as New does.
func (r *Registry) insert(m *manifest.Manifest) error {
	if _, dup := r.byName[m.Metadata.Name]; dup {
		return fmt.Errorf("duplicate plugin name %q", m.Metadata.Name)
	}
	for _, capURI := range m.ProvidesCaps() {
		if other, dup := r.providerOf[capURI]; dup {
			return fmt.Errorf("capability %q provided by both %q and %q (no provider-selection policy in the spike)", capURI, other, m.Metadata.Name)
		}
	}
	r.byName[m.Metadata.Name] = m
	for _, capURI := range m.ProvidesCaps() {
		r.providerOf[capURI] = m.Metadata.Name
	}
	return nil
}

// Register adds a manifest to a RUNNING registry (the live ControlService, ADR-027).
// Same invariants as New, enforced at runtime: a duplicate name or duplicate capability
// provider is rejected, never silently overridden. Concurrency-safe.
func (r *Registry) Register(m *manifest.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.insert(m)
}

// Deregister removes a plugin (and the capabilities it provided) from a running
// registry. A no-op if the name is absent. Concurrency-safe.
func (r *Registry) Deregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.byName[name]
	if m == nil {
		return
	}
	for _, capURI := range m.ProvidesCaps() {
		if r.providerOf[capURI] == name {
			delete(r.providerOf, capURI)
		}
	}
	delete(r.byName, name)
}

// Plugin returns the manifest registered under name, or nil.
func (r *Registry) Plugin(name string) *manifest.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byName[name]
}

// ProviderOf returns the plugin name that provides capURI, or "".
func (r *Registry) ProviderOf(capURI string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providerOf[capURI]
}

// Authorize is the C5 decision. A call to capURI by plugin caller is allowed iff
// the caller's manifest `requires` it AND some registered plugin `provides` it.
// The decision is derived entirely from declared manifests — nothing is hardcoded.
func (r *Registry) Authorize(caller, capURI string) Decision {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cm := r.byName[caller]
	if cm == nil {
		return Decision{Reason: fmt.Sprintf("C5: unknown caller %q", caller)}
	}
	if !contains(cm.RequiresCaps(), capURI) {
		return Decision{Reason: fmt.Sprintf("C5: caller %q does not declare `requires` %q", caller, capURI)}
	}
	provider := r.providerOf[capURI]
	if provider == "" {
		return Decision{Reason: fmt.Sprintf("C5: no registered plugin provides %q", capURI)}
	}
	return Decision{
		Allowed:  true,
		Provider: provider,
		Reason:   fmt.Sprintf("%s requires %s; %s provides it", caller, capURI, provider),
	}
}

// All returns every registered manifest, sorted by name (stable output) — the live
// plane the ControlService.ListPlugins reports. Concurrency-safe.
func (r *Registry) All() []*manifest.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*manifest.Manifest, 0, len(names))
	for _, n := range names {
		out = append(out, r.byName[n])
	}
	return out
}

// Capabilities returns every provided capability URI, sorted (stable output).
func (r *Registry) Capabilities() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providerOf))
	for c := range r.providerOf {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
