// Package registry is the spike core's manifest-driven plugin registry (ADR-014).
//
// It loads plugin manifests and answers two questions the capability-invoke
// gateway asks on every call: "who provides capability X?" and "is caller P
// authorized to invoke X (and which provider serves this call)?". The
// authorization decision is DERIVED from the plugins' own declared manifests —
// caller.requires ∧ provider.provides — not a hardcoded allowlist.
//
// PROVIDER SELECTION (ADR-045). A capability may have MORE THAN ONE provider —
// e.g. engine-duckdb (compute=small) and engine-spark (compute=big) both
// providing rat://engine/v1/execute. Eligibility (who CAN serve) stays the
// capability negotiation above; SELECTION (which one serves THIS call) is by
// LABEL: each provider carries labels (manifest self-description + plane
// override), and a call may carry a selector (a set of key=value matched with
// AND). Selection is deterministic and fails CLOSED — a selector matching zero
// or >1 distinct providers is refused, never resolved arbitrarily.
package registry

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/le-squat/rat/core/manifest"
)

// Decision is the result of a C5 authorization + ADR-045 selection check. The gateway turns
// it into either a proxied call (Allowed) or a status error, and audits it.
type Decision struct {
	// Allowed: authorized AND a UNIQUE provider was selected → route to Provider.
	Allowed bool
	// Authorized: the caller passed C5 (it `requires` the capability AND some plugin provides
	// it). Distinguishes a C5 DENY (Authorized=false → PermissionDenied) from a SELECTION
	// failure (Authorized=true, Allowed=false → FailedPrecondition: no/ambiguous label match).
	Authorized bool
	// Provider: the selected providing plugin ("" if denied or selection failed).
	Provider string
	// Reason: human-readable rationale (always set; the audit/deny message).
	Reason string
}

// Registry indexes loaded manifests by plugin name and by provided capability. It is
// concurrency-safe: the live ControlService mutates it (Register/Deregister, ADR-027)
// while the gateway reads it (Authorize/Select) on every call.
type Registry struct {
	mu         sync.RWMutex
	byName     map[string]*manifest.Manifest
	providerOf map[string][]string // capability URI -> providing plugin names (a SET — ADR-045)
}

// New builds a registry from a set of manifests. It rejects duplicate plugin names. Unlike
// the spike, it ACCEPTS multiple providers of one capability (ADR-045): they coexist and
// selection (by label) disambiguates per call.
func New(manifests []*manifest.Manifest) (*Registry, error) {
	r := &Registry{
		byName:     make(map[string]*manifest.Manifest, len(manifests)),
		providerOf: make(map[string][]string),
	}
	for _, m := range manifests {
		if err := r.insert(m); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// insert adds one manifest, enforcing the no-dup-NAME invariant. Multiple providers of one
// capability are allowed (ADR-045) — they are appended to the capability's provider set. The
// caller holds the write lock — or owns r exclusively, as New does.
func (r *Registry) insert(m *manifest.Manifest) error {
	if _, dup := r.byName[m.Metadata.Name]; dup {
		return fmt.Errorf("duplicate plugin name %q", m.Metadata.Name)
	}
	r.byName[m.Metadata.Name] = m
	for _, capURI := range m.ProvidesCaps() {
		r.providerOf[capURI] = append(r.providerOf[capURI], m.Metadata.Name)
	}
	return nil
}

// Register adds a manifest to a RUNNING registry (the live ControlService, ADR-027). Same
// invariant as New: a duplicate name is rejected; a second provider of an existing capability
// is now ACCEPTED (it coexists, ADR-045). Concurrency-safe.
func (r *Registry) Register(m *manifest.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.insert(m)
}

// Deregister removes a plugin (and drops it from the capabilities it provided) from a running
// registry. A no-op if the name is absent. Concurrency-safe.
func (r *Registry) Deregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.byName[name]
	if m == nil {
		return
	}
	for _, capURI := range m.ProvidesCaps() {
		r.providerOf[capURI] = removeString(r.providerOf[capURI], name)
		if len(r.providerOf[capURI]) == 0 {
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

// ProviderOf returns the lexicographically-first plugin that provides capURI, or "". A
// convenience for the common single-provider case; use Select for label-aware resolution.
func (r *Registry) ProviderOf(capURI string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ps := r.providerOf[capURI]
	if len(ps) == 0 {
		return ""
	}
	return sortedCopy(ps)[0]
}

// ProvidersOf returns every plugin that provides capURI, sorted (stable output).
func (r *Registry) ProvidersOf(capURI string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return sortedCopy(r.providerOf[capURI])
}

// Authorize is the selectorless C5 decision (back-compat): a call to capURI by plugin caller
// is allowed iff the caller `requires` it AND exactly one plugin provides it. With multiple
// providers it returns an ambiguous (not-Allowed) decision — the caller must Select with a
// selector. Equivalent to Select(caller, capURI, nil).
func (r *Registry) Authorize(caller, capURI string) Decision {
	return r.Select(caller, capURI, nil)
}

// Select is the C5 authorization + ADR-045 provider selection. It authorizes the caller
// (requires ∧ some provider provides) and then SELECTS the provider whose labels satisfy the
// selector (a set of key=value matched with AND; a nil/empty selector matches every provider).
// The decision is derived entirely from declared manifests — nothing is hardcoded.
//
// Outcomes: a unique match → Allowed with that Provider; zero matches or >1 distinct matches →
// Authorized but NOT Allowed (selection fails CLOSED — the operator refines the selector); an
// unknown/under-declared caller or a capability no plugin provides → not Authorized (C5 deny).
func (r *Registry) Select(caller, capURI string, selector map[string]string) Decision {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cm := r.byName[caller]
	if cm == nil {
		return Decision{Reason: fmt.Sprintf("C5: unknown caller %q", caller)}
	}
	if !contains(cm.RequiresCaps(), capURI) {
		return Decision{Reason: fmt.Sprintf("C5: caller %q does not declare `requires` %q", caller, capURI)}
	}
	providers := r.providerOf[capURI]
	if len(providers) == 0 {
		return Decision{Reason: fmt.Sprintf("C5: no registered plugin provides %q", capURI)}
	}

	// Authorized (C5 passes). Now SELECT by label (ADR-045).
	matched := make([]string, 0, len(providers))
	for _, p := range providers {
		if labelsMatch(r.byName[p].Metadata.Labels, selector) {
			matched = append(matched, p)
		}
	}
	sort.Strings(matched)
	switch len(matched) {
	case 1:
		return Decision{
			Allowed: true, Authorized: true, Provider: matched[0],
			Reason: fmt.Sprintf("%s requires %s; selected %s%s", caller, capURI, matched[0], selectorNote(selector)),
		}
	case 0:
		return Decision{
			Authorized: true,
			Reason: fmt.Sprintf("selection: no provider of %q matches selector %s (providers: %s)",
				capURI, fmtSelector(selector), strings.Join(sortedCopy(providers), ", ")),
		}
	default:
		return Decision{
			Authorized: true,
			Reason: fmt.Sprintf("selection: selector %s is ambiguous for %q — matches %s; refine it",
				fmtSelector(selector), capURI, strings.Join(matched, ", ")),
		}
	}
}

// All returns every registered manifest, sorted by name (stable output) — the live plane the
// ControlService.ListPlugins reports. Concurrency-safe.
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

// labelsMatch reports whether labels satisfy selector: every key=value in selector is present
// and equal in labels (AND). A nil/empty selector matches everything.
func labelsMatch(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func selectorNote(selector map[string]string) string {
	if len(selector) == 0 {
		return ""
	}
	return " for selector " + fmtSelector(selector)
}

// fmtSelector renders a selector as a stable "k=v,k=v" string (sorted keys) for messages.
func fmtSelector(selector map[string]string) string {
	if len(selector) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(selector))
	for k := range selector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+selector[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func sortedCopy(ss []string) []string {
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}

func removeString(ss []string, s string) []string {
	out := ss[:0]
	for _, x := range ss {
		if x != s {
			out = append(out, x)
		}
	}
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
