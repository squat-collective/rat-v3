// Package main — rat-engine-inmemory-go: a reference `kind: engine` plugin.
//
// Sub-phase-0d reference implementation (ADR-003): its job is to exercise +
// validate the engine/v1 wire contract, not to be a production query engine. It
// is a SELF-CONTAINED in-memory mini-SQL engine — it holds its own tables rather
// than querying a format/storage provider. (The real engine↔format handoff, where
// the engine pulls an Arrow stream from a format plugin via the core gateway, is a
// separate multi-axis integration concern, not a single-axis 0d reference.)
//
// "Bulk" query results that the contract says flow out-of-band as Arrow are, for
// this reference, carried inline through the in-process stream registry
// (stream.go) — the point is to prove the CONTROL-plane contract (Execute / Query
// / Preview), with a stand-in for the data leg, so a second independent impl +
// golden vectors can cross-check behavior.
package main

import "sync"

// row is one record: column -> value. Values are strings (the mini-SQL is
// untyped) to keep the reference trivial and language-neutral.
type row map[string]string

// table is an in-memory relation: ordered columns + insertion-ordered rows.
type table struct {
	cols []string
	rows []row
}

// store is the engine's whole state. Concurrency-safe for the gRPC threadpool. A
// single monotonic snapshot counter is bumped on every mutation and surfaced as
// WriteResult.snapshot_id.
type store struct {
	mu       sync.Mutex
	tables   map[string]*table
	snapshot int64
}

func newStore() *store { return &store{tables: map[string]*table{}} }

// create (re)declares a table with the given columns. CREATE is idempotent-ish:
// re-creating replaces. Returns the new snapshot.
func (s *store) create(name string, cols []string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tables[name] = &table{cols: append([]string(nil), cols...)}
	s.snapshot++
	return s.snapshot
}

// insert binds values positionally to the table's columns and appends a row.
// Returns (rows_affected=1, snapshot) or an error if the table is unknown.
func (s *store) insert(name string, vals []string) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tables[name]
	if !ok {
		return 0, 0, errUnknownTable(name)
	}
	r := row{}
	for i, c := range t.cols {
		if i < len(vals) {
			r[c] = vals[i]
		}
	}
	t.rows = append(t.rows, r)
	s.snapshot++
	return 1, s.snapshot, nil
}

// selectRows applies an optional WHERE equality filter and a projection, returning
// rows in insertion order. Projection ["*"] yields all columns.
func (s *store) selectRows(st stmt) ([]row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tables[st.table]
	if !ok {
		return nil, errUnknownTable(st.table)
	}
	out := make([]row, 0, len(t.rows))
	for _, r := range t.rows {
		if st.hasWhere && r[st.whereCol] != st.whereVal {
			continue
		}
		out = append(out, project(r, st.cols, t.cols))
	}
	return out, nil
}

// project returns a copy of r restricted to the projection columns (or all
// columns when the projection is ["*"]).
func project(r row, proj, allCols []string) row {
	cols := proj
	if len(proj) == 1 && proj[0] == "*" {
		cols = allCols
	}
	out := make(row, len(cols))
	for _, c := range cols {
		if v, ok := r[c]; ok {
			out[c] = v
		}
	}
	return out
}
