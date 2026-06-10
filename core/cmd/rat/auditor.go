package main

import (
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/squat-collective/rat-v3/core/gateway"
)

// StdoutAuditor is the daemon's audit sink: it implements gateway.Auditor by
// writing ONE JSON line per audit record to an io.Writer (os.Stdout by default).
// It exists so the ADR-001 "mandatory audit emission, even with no audit-log
// plugin installed" invariant holds for `rat serve` out of the box — a real
// audit-log plugin (the audit-log axis) replaces it later (ADR-019 decision 3).
//
// The gateway emits records from multiple goroutines (a server-stream's terminal
// record fires from the relay goroutine while another call records its decision),
// so writes are mutex-serialized to keep lines intact.
type StdoutAuditor struct {
	mu sync.Mutex
	w  io.Writer
}

// NewStdoutAuditor writes to w; a nil w defaults to os.Stdout.
func NewStdoutAuditor(w io.Writer) *StdoutAuditor {
	if w == nil {
		w = os.Stdout
	}
	return &StdoutAuditor{w: w}
}

// auditLine is the on-the-wire JSON shape — the spike gateway.AuditRecord fields
// plus a "kind" discriminator so a decision record and a stream's terminal record
// are distinguishable in the log. (The frozen common/v1.AuditRecord with the core
// signature + hash chain is the GA sink; this is the daemon's honest minimum.)
type auditLine struct {
	Kind        string `json:"kind"` // "decision" | "stream-close"
	Capability  string `json:"capability"`
	Caller      string `json:"caller"`
	Provider    string `json:"provider,omitempty"`
	Correlation string `json:"correlation,omitempty"`
	Allowed     bool   `json:"allowed"`
	Reason      string `json:"reason,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Frames      int    `json:"frames,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Record marshals r to one JSON line. A marshal error is itself surfaced as a
// line (the audit trail must never silently drop a decision — ADR-001).
func (a *StdoutAuditor) Record(r gateway.AuditRecord) {
	kind := "decision"
	if r.Terminal {
		kind = "stream-close"
	}
	b, err := json.Marshal(auditLine{
		Kind: kind, Capability: r.Capability, Caller: r.Caller, Provider: r.Provider,
		Correlation: r.Correlation, Allowed: r.Allowed, Reason: r.Reason,
		Outcome: r.Outcome, Frames: r.Frames, Error: r.Error,
	})
	if err != nil {
		b = []byte(`{"kind":"audit-marshal-error"}`)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, _ = a.w.Write(append(b, '\n'))
}
