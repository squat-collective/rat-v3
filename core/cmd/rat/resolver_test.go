package main

import (
	"testing"

	"github.com/rat-dev/rat/core/manifest"
)

func mkManifest(name string, provides, requires []string) *manifest.Manifest {
	m := &manifest.Manifest{Metadata: manifest.Metadata{Name: name}}
	for _, c := range provides {
		m.Provides = append(m.Provides, manifest.CapabilityRef{Capability: c})
	}
	for _, c := range requires {
		m.Requires = append(m.Requires, manifest.CapabilityRef{Capability: c})
	}
	return m
}

// TestUnsatisfiedRequires covers the plane-level satisfiability resolver: a requires is
// satisfied iff some plugin in the set provides it (cross-plugin).
func TestUnsatisfiedRequires(t *testing.T) {
	pipe := mkManifest("pipe", []string{"rat://strategy/v1/apply"}, []string{"rat://state/v1/get", "rat://secret/v1/resolve"})
	state := mkManifest("state", []string{"rat://state/v1/get"}, []string{"rat://secret/v1/resolve"})
	secret := mkManifest("secret", []string{"rat://secret/v1/resolve"}, nil)

	// full set → everything resolves.
	if m := unsatisfiedRequires([]*manifest.Manifest{pipe, state, secret}); len(m) != 0 {
		t.Errorf("full set should be satisfied, got %+v", m)
	}

	// drop secret → both pipe and state lose secret/resolve.
	miss := unsatisfiedRequires([]*manifest.Manifest{pipe, state})
	if len(miss) != 2 {
		t.Fatalf("expected 2 unsatisfied, got %+v", miss)
	}
	for _, d := range miss {
		if d.Capability != "rat://secret/v1/resolve" {
			t.Errorf("unexpected missing cap %q", d.Capability)
		}
	}

	// pipe alone → state/get + secret/resolve both missing.
	if m := unsatisfiedRequires([]*manifest.Manifest{pipe}); len(m) != 2 {
		t.Errorf("pipe alone should miss 2, got %+v", m)
	}
}
