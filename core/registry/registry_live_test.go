package registry

import (
	"fmt"
	"sync"
	"testing"

	"github.com/squat-collective/rat-v3/core/manifest"
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

// TestRegisterRejectsDuplicateName: the runtime path still rejects a duplicate plugin NAME and
// leaves NO partial state. A second PROVIDER of an existing capability is now ACCEPTED (ADR-045):
// providers coexist and selection disambiguates.
func TestRegisterRejectsDuplicateName(t *testing.T) {
	reg, _ := New([]*manifest.Manifest{
		man("a", "engine", []string{"rat://engine/v1/execute"}, nil),
	})
	if err := reg.Register(man("a", "engine", nil, nil)); err == nil {
		t.Fatal("duplicate plugin name should be rejected")
	}
	if reg.Plugin("a") == nil {
		t.Fatal("rejected dup-name register corrupted the existing plugin")
	}
	// A second provider of the same capability now coexists (ADR-045).
	if err := reg.Register(man("b", "engine", []string{"rat://engine/v1/execute"}, nil)); err != nil {
		t.Fatalf("second provider of a capability should be accepted (ADR-045): %v", err)
	}
	if got := reg.ProvidersOf("rat://engine/v1/execute"); len(got) != 2 {
		t.Fatalf("ProvidersOf after coexisting register = %v, want [a b]", got)
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
