// Package main — rat-format-inmemory-go: a reference `kind: format` plugin.
//
// This is a sub-phase-0d reference implementation (ADR-003): its job is to
// exercise + validate the format/v1 wire contract, not to be production storage.
// It keeps tables in memory as ordered row maps. "Bulk data" that the contract
// says flows out-of-band as Arrow is, for this reference, carried inline through
// a trivial in-process stream registry (see stream.go) — the point is to prove
// the CONTROL-plane contract (Resolve/Append/Merge/Overwrite/Maintain), with a
// stand-in for the data leg, so a second independent impl + golden vectors can
// cross-check behavior.
package main

import (
	"sort"
	"sync"
)

// row is one record: a map of column -> value. Values are strings to keep the
// reference trivial and language-neutral (the real Arrow handoff carries typed
// columns; this stand-in does not need types to validate the control contract).
type row map[string]string

// table is an in-memory dataset addressed by a TableRef.identifier.
type table struct {
	rows     []row
	snapshot int64 // bumped on every mutation; surfaced as WriteResult.snapshot_id
}

// store is the plugin's whole state: identifier -> table. Concurrency-safe so the
// gRPC server can serve parallel calls.
type store struct {
	mu     sync.Mutex
	tables map[string]*table
}

func newStore() *store {
	return &store{tables: map[string]*table{}}
}

func (s *store) get(identifier string) *table {
	t, ok := s.tables[identifier]
	if !ok {
		t = &table{}
		s.tables[identifier] = t
	}
	return t
}

// append adds rows unconditionally. Returns rows affected + new snapshot id.
func (s *store) append(identifier string, rows []row) (int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.get(identifier)
	t.rows = append(t.rows, rows...)
	t.snapshot++
	return int64(len(rows)), t.snapshot
}

// overwrite replaces all rows. Returns rows affected + new snapshot id.
func (s *store) overwrite(identifier string, rows []row) (int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.get(identifier)
	t.rows = append([]row(nil), rows...)
	t.snapshot++
	return int64(len(rows)), t.snapshot
}

// merge upserts rows matched on mergeKeys: a source row replaces the first
// existing row whose mergeKey columns all match, else it is appended. Returns
// rows affected (updated + inserted) + new snapshot id.
func (s *store) merge(identifier string, mergeKeys []string, src []row) (int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.get(identifier)
	var affected int64
	for _, sr := range src {
		idx := -1
		for i, existing := range t.rows {
			if keysMatch(existing, sr, mergeKeys) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			t.rows[idx] = sr
		} else {
			t.rows = append(t.rows, sr)
		}
		affected++
	}
	t.snapshot++
	return affected, t.snapshot
}

// scan returns a copy of the table's rows, optionally projecting columns. The
// rows are returned in a stable order (insertion order is preserved) so golden
// vectors are deterministic.
func (s *store) scan(identifier string, columns []string) []row {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.get(identifier)
	out := make([]row, 0, len(t.rows))
	for _, r := range t.rows {
		if len(columns) == 0 {
			cp := make(row, len(r))
			for k, v := range r {
				cp[k] = v
			}
			out = append(out, cp)
			continue
		}
		cp := make(row, len(columns))
		for _, c := range columns {
			if v, ok := r[c]; ok {
				cp[c] = v
			}
		}
		out = append(out, cp)
	}
	return out
}

// maintain is a no-op upkeep for the in-memory store (nothing to compact). It
// still bumps the snapshot so the contract's "returns a WriteResult" holds.
func (s *store) maintain(identifier string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.get(identifier)
	t.snapshot++
	return t.snapshot
}

func keysMatch(a, b row, keys []string) bool {
	for _, k := range keys {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

// sortedKeys is a small helper for deterministic column ordering in tests.
func sortedKeys(r row) []string {
	ks := make([]string, 0, len(r))
	for k := range r {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
