package registry

import (
	"fmt"
	"sync"
	"testing"

	"github.com/rat-dev/rat/core/manifest"
)

func man(name, kind string, provides, requires []string) *manifest.Manifest {
	m := &manifest.Manifest{Kind: kind}
	m.Metadata.Name = name
	for _, c := range provides {
		m.Provides = append(m.Provides, manifest.CapabilityRef{Capability: c})
	}
	for _, c := range requires {
		m.Requires = append(m.Requires, manifest.CapabilityRef{Capability: c})
	}
	return m
}

// TestRegisterDeregister: a plugin added to a RUNNING registry becomes authorizable, and
// removing it retracts its capabilities — the live-control invariant (ADR-027).
func TestRegisterDeregister(t *testing.T) {
	reg, err := New(nil) // empty running registry
	if err != nil {
		t.Fatalf("New(nil): %v", err)
	}

	caller := man("rat-scheduler", "scheduler-backend", nil, []string{"rat://state/v1/put"})
	provider := man("rat-state", "state-backend", []string{"rat://state/v1/put"}, nil)
	if err := reg.Register(caller); err != nil {
		t.Fatalf("Register caller: %v", err)
	}
	// Before the provider exists, the capability is unprovided → deny.
	if d := reg.Authorize("rat-scheduler", "rat://state/v1/put"); d.Allowed {
		t.Fatal("authorize should DENY before the provider is registered")
	}
	if err := reg.Register(provider); err != nil {
		t.Fatalf("Register provider: %v", err)
	}
	// Now the live registry authorizes the call.
	if d := reg.Authorize("rat-scheduler", "rat://state/v1/put"); !d.Allowed || d.Provider != "rat-state" {
		t.Fatalf("authorize after register = %+v, want allowed via rat-state", d)
	}
	if got := reg.ProviderOf("rat://state/v1/put"); got != "rat-state" {
		t.Fatalf("ProviderOf = %q, want rat-state", got)
	}

	// Deregister retracts the provider + its capability.
	reg.Deregister("rat-state")
	if got := reg.ProviderOf("rat://state/v1/put"); got != "" {
		t.Fatalf("ProviderOf after deregister = %q, want empty", got)
	}
	if d := reg.Authorize("rat-scheduler", "rat://state/v1/put"); d.Allowed {
		t.Fatal("authorize should DENY after the provider is deregistered")
	}
	reg.Deregister("nope") // absent → no-op, no panic
}

// TestRegisterRejectsDuplicates: the runtime path keeps New's invariants (no dup name, no
// dup provider) — and a rejected register leaves NO partial state.
func TestRegisterRejectsDuplicates(t *testing.T) {
	reg, _ := New([]*manifest.Manifest{
		man("a", "engine", []string{"rat://engine/v1/execute"}, nil),
	})
	if err := reg.Register(man("a", "engine", nil, nil)); err == nil {
		t.Fatal("duplicate plugin name should be rejected")
	}
	// A second provider of an existing capability is rejected, and must not corrupt the
	// existing mapping (atomic insert).
	if err := reg.Register(man("b", "engine", []string{"rat://engine/v1/execute"}, nil)); err == nil {
		t.Fatal("duplicate capability provider should be rejected")
	}
	if got := reg.ProviderOf("rat://engine/v1/execute"); got != "a" {
		t.Fatalf("rejected register corrupted the provider index: got %q, want a", got)
	}
	if reg.Plugin("b") != nil {
		t.Fatal("rejected register must not leave the plugin in byName")
	}
}

// TestRegistryRace exercises concurrent Register/Deregister against concurrent
// Authorize/ProviderOf reads (run under -race). Distinct names/capabilities per worker so
// the only contention is on the registry's locks, not on the no-dup invariant.
func TestRegistryRace(t *testing.T) {
	reg, _ := New(nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		name := fmt.Sprintf("p%d", i)
		cap := fmt.Sprintf("rat://engine/v1/m%d", i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = reg.Register(man(name, "engine", []string{cap}, nil))
				reg.Deregister(name)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = reg.ProviderOf(cap)
				_ = reg.Authorize("x", cap)
				_ = reg.Capabilities()
			}
		}()
	}
	wg.Wait()
}
