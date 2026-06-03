package main

import (
	"os"
	"testing"
)

// TestRegistryAndPid covers the slice-2c instance registry: pid liveness, upsert-by-dir
// (replace), prune-dead, and remove. Uses a temp XDG_STATE_HOME so it touches no real state.
func TestRegistryAndPid(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if !pidAlive(os.Getpid()) {
		t.Error("own pid should read as alive")
	}
	if pidAlive(1 << 30) {
		t.Error("a bogus pid should read as dead")
	}

	registryUpsert(instanceEntry{Name: "alive", Dir: "/p/alive", Pid: os.Getpid(), Addr: "unix:/p/alive/.rat/sock"})
	registryUpsert(instanceEntry{Name: "dead", Dir: "/p/dead", Pid: 1 << 30, Addr: "x"})

	// prune drops the dead one, keeps the live one.
	es := pruneRegistry(loadRegistry())
	if len(es) != 1 || es[0].Dir != "/p/alive" {
		t.Fatalf("prune = %+v, want only /p/alive", es)
	}

	// upsert of the same dir REPLACES (not duplicates).
	registryUpsert(instanceEntry{Name: "alive2", Dir: "/p/alive", Pid: os.Getpid(), Addr: "y"})
	n, name := 0, ""
	for _, e := range loadRegistry() {
		if e.Dir == "/p/alive" {
			n, name = n+1, e.Name
		}
	}
	if n != 1 || name != "alive2" {
		t.Errorf("upsert same dir: count=%d name=%q, want 1 / alive2", n, name)
	}

	// remove drops it.
	registryRemove("/p/alive")
	for _, e := range loadRegistry() {
		if e.Dir == "/p/alive" {
			t.Error("registryRemove did not remove /p/alive")
		}
	}
}
