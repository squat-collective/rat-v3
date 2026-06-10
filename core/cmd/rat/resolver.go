package main

// resolver.go — the SATISFIABILITY resolver (ADR-023 #6 / ADR-026 follow-on). `rat plugin
// check` validates a single plugin's deps are REAL (coherence); this is the complementary
// PLANE-level check: across the plugins in a project/plane, is every `requires` actually
// PROVIDED by some plugin in the set? An unsatisfied requires means the gateway has no route
// for that capability — the plugin will fail if it calls it. Surfaced (poetry-style) at
// `rat add` and `rat up`, with a suggestion.

import (
	"fmt"
	"io"
	"log"

	"github.com/le-squat/rat/core/manifest"
)

// missingDep is one unsatisfied dependency: <plugin> requires <capability>, nobody provides it.
type missingDep struct {
	Plugin     string
	Capability string
}

// unsatisfiedRequires returns every `requires` across the manifests that no manifest provides.
func unsatisfiedRequires(ms []*manifest.Manifest) []missingDep {
	provided := map[string]bool{}
	for _, m := range ms {
		for _, c := range m.ProvidesCaps() {
			provided[c] = true
		}
	}
	var miss []missingDep
	for _, m := range ms {
		for _, c := range m.RequiresCaps() {
			if !provided[c] {
				miss = append(miss, missingDep{Plugin: m.Metadata.Name, Capability: c})
			}
		}
	}
	return miss
}

// reportUnsatisfied prints a poetry-style warning for each unsatisfied dependency (with a
// suggestion = the axis a provider would belong to). No-op when everything resolves.
func reportUnsatisfied(out io.Writer, miss []missingDep) {
	if len(miss) == 0 {
		return
	}
	fmt.Fprintf(out, "⚠ %d unsatisfied dependenc%s (a `requires` no plugin in this project provides):\n", len(miss), plural(len(miss)))
	for _, d := range miss {
		fmt.Fprintf(out, "   %s requires %s — no provider (add a %s-axis plugin)\n", d.Plugin, d.Capability, capAxisOf(d.Capability))
	}
}

// logUnsatisfied is the daemon-side variant (rat up) — same check, via the log.
func logUnsatisfied(ms []*manifest.Manifest) {
	for _, d := range unsatisfiedRequires(ms) {
		log.Printf("⚠ unsatisfied: %s requires %s — no provider in the plane (add a %s-axis plugin)", d.Plugin, d.Capability, capAxisOf(d.Capability))
	}
}
