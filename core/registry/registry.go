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

// Registry indexes loaded manifests by plugin name and by provided capability.
type Registry struct {
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
		if _, dup := r.byName[m.Metadata.Name]; dup {
			return nil, fmt.Errorf("duplicate plugin name %q", m.Metadata.Name)
		}
		r.byName[m.Metadata.Name] = m
		for _, capURI := range m.ProvidesCaps() {
			if other, dup := r.providerOf[capURI]; dup {
				return nil, fmt.Errorf("capability %q provided by both %q and %q (no provider-selection policy in the spike)", capURI, other, m.Metadata.Name)
			}
			r.providerOf[capURI] = m.Metadata.Name
		}
	}
	return r, nil
}

// Plugin returns the manifest registered under name, or nil.
func (r *Registry) Plugin(name string) *manifest.Manifest { return r.byName[name] }

// ProviderOf returns the plugin name that provides capURI, or "".
func (r *Registry) ProviderOf(capURI string) string { return r.providerOf[capURI] }

// Authorize is the C5 decision. A call to capURI by plugin caller is allowed iff
// the caller's manifest `requires` it AND some registered plugin `provides` it.
// The decision is derived entirely from declared manifests — nothing is hardcoded.
func (r *Registry) Authorize(caller, capURI string) Decision {
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

// Capabilities returns every provided capability URI, sorted (stable output).
func (r *Registry) Capabilities() []string {
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
