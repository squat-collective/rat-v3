// Package composition is the spike's cross-axis composition test (ADR-014 §5):
// it drives the real pipeline call-sequence (catalog get-table → format overwrite
// → catalog commit-table) through the core's enforcing gateway, with a manifest
// per plugin, proving the frozen wire + the manifest-derived C5 enforcement compose
// end-to-end across axes — and that a crash mid-strategy recovers without
// double-applying (C1, ADR-012).
//
// The providers here are in-Go fakes that honor the frozen RPCs + the idempotency
// contract; real-backend equivalence (DuckDB/Parquet) is already proven by the
// Python examples/composition. The NEW signal this adds is the Go *enforcing*
// gateway mediating a multi-axis pipeline + the crash-safety recovery path.
//
// The package holds only tests; this file documents it.
package composition
