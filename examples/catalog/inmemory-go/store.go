// Package main — rat-catalog-inmemory-go: a reference `kind: catalog` plugin.
//
// Sub-phase-0d reference (ADR-003). A catalog owns table metadata + git-like
// branch/version semantics — the basis for branch-isolated pipeline runs. This
// reference models branches GLOBALLY (a branch is a named snapshot of the catalog,
// like a git branch), seeds one table on `main`, and implements the MERGE-SAFETY
// contract (reviews/06 #8): optimistic concurrency (expected_into_snapshot) +
// idempotency (idempotency_key → already_applied). It is not production metadata
// storage; the point under test is the branch/merge WIRE contract.
package main

import (
	"fmt"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const seedTable = "warehouse.sales.orders"

// catalog state: global branches (branch → current snapshot id), the set of known
// table identifiers, and a committed-merge ledger keyed by idempotency_key.
type catalog struct {
	mu       sync.Mutex
	branches map[string]string // branch -> snapshot id (e.g. "snap-3")
	tables   map[string]bool   // known table identifiers
	merges   map[string]string // idempotency_key -> resulting snapshot id (idempotency ledger)
	counter  int64             // monotonic; each merge mints snap-<counter>
}

func newCatalog() *catalog {
	return &catalog{
		branches: map[string]string{"main": "snap-0"}, // seed: main at snap-0
		tables:   map[string]bool{seedTable: true},     // seed: one known table
		merges:   map[string]string{},
	}
}

func (c *catalog) getTable(identifier, branch string) (resolvedBranch, uri string, err error) {
	if identifier == "" {
		return "", "", status.Error(codes.InvalidArgument, "identifier is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.tables[identifier] {
		return "", "", status.Errorf(codes.NotFound, "unknown table %q", identifier)
	}
	if branch == "" {
		branch = "main"
	}
	if _, ok := c.branches[branch]; !ok {
		return "", "", status.Errorf(codes.NotFound, "unknown branch %q", branch)
	}
	return branch, fmt.Sprintf("catalog://%s@%s", identifier, branch), nil
}

func (c *catalog) createBranch(branch, from string) error {
	if branch == "" {
		return status.Error(codes.InvalidArgument, "branch is required")
	}
	if from == "" {
		from = "main"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	src, ok := c.branches[from]
	if !ok {
		return status.Errorf(codes.NotFound, "unknown from_branch %q", from)
	}
	c.branches[branch] = src // branch copies the source's snapshot
	return nil
}

// mergeBranch applies the MERGE-SAFETY contract. Returns (snapshot, alreadyApplied).
func (c *catalog) mergeBranch(branch, into, expected, key string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Idempotency: a key that already committed is a no-op returning the original.
	if key != "" {
		if snap, ok := c.merges[key]; ok {
			return snap, true, nil
		}
	}
	if _, ok := c.branches[branch]; !ok {
		return "", false, status.Errorf(codes.NotFound, "unknown branch %q", branch)
	}
	cur, ok := c.branches[into]
	if !ok {
		return "", false, status.Errorf(codes.NotFound, "unknown into_branch %q", into)
	}
	// Optimistic concurrency: apply only if into_branch is still at `expected`.
	if expected != "" && expected != cur {
		return "", false, status.Errorf(codes.FailedPrecondition,
			"into_branch %q is at %q, not the expected %q (concurrent merge?)", into, cur, expected)
	}
	c.counter++
	snap := fmt.Sprintf("snap-%d", c.counter)
	c.branches[into] = snap
	if key != "" {
		c.merges[key] = snap
	}
	return snap, false, nil
}
