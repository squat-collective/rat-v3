// Package main — rat-runtime-inmemory-go: a reference `kind: runtime` plugin.
//
// Sub-phase-0d reference (ADR-003). A runtime executes a unit of work and streams
// liveness back. This reference runs a deliberately tiny work_spec so two
// independent implementations exercise the STREAMING wire contract identically —
// the point under test is the progress/completed framing + optional-fraction
// presence, not real compute. The real "where code runs" substance (a container
// exec, a WASM host, pyarrow) is out of scope for a contract-validation reference.
package main

import (
	"encoding/json"
	"errors"
)

// workSpec is the reference's tiny, fully-specified work format (JSON). A real
// runtime treats work_spec as opaque bytes it alone interprets; the reference
// interprets these four fields.
type workSpec struct {
	Steps         int    `json:"steps"`         // number of progress messages to emit
	Rows          int64  `json:"rows"`          // rows_affected in the terminal result
	Indeterminate bool   `json:"indeterminate"` // if true, progress omits fraction (absent)
	Fail          string `json:"fail"`          // if set, terminate success=false with this error
}

var errEmptySpec = errors.New("work_spec is required")

func parseWorkSpec(b []byte) (workSpec, error) {
	if len(b) == 0 {
		return workSpec{}, errEmptySpec
	}
	var s workSpec
	if err := json.Unmarshal(b, &s); err != nil {
		return workSpec{}, err
	}
	return s, nil
}
