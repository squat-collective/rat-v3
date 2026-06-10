package main

import (
	"strings"
	"testing"
)

// `rat capabilities` (DX-3) renders the in-binary registry — the same annotations
// `rat plugin check` and the gateway enforce — so it can never drift from reality.

func TestAllCapabilitiesKnowsTheWire(t *testing.T) {
	byURI := map[string]capInfo{}
	for _, c := range allCapabilities() {
		byURI[c.URI] = c
	}
	get, ok := byURI["rat://state/v1/get"]
	if !ok {
		t.Fatalf("rat://state/v1/get missing from the registry")
	}
	if get.Method != "Get" || get.Cardinality != "unary" || get.In != "GetRequest" {
		t.Fatalf("get wrong: %+v", get)
	}
	watch := byURI["rat://state/v1/watch"]
	if watch.Cardinality != "server-stream" {
		t.Fatalf("watch should be server-stream, got %+v", watch)
	}
	// The ADR-049 amendment is in the registry too — the listing tracks the wire.
	if _, ok := byURI["rat://state/v1/create-if-absent"]; !ok {
		t.Fatalf("create-if-absent (ADR-049) missing")
	}
}

func TestAxisKindRoundTrip(t *testing.T) {
	for axis, kind := range map[string]string{
		"state":     "state-backend",
		"secret":    "secret-backend",
		"scheduler": "scheduler-backend",
		"auditlog":  "audit-log",
		// the frozen-wire wart: this URI axis keeps the hyphen (dir doesn't)
		"deployment-runtime": "deployment-runtime",
		"engine":             "engine",
	} {
		if got := axisKind(axis); got != kind {
			t.Fatalf("axisKind(%q) = %q, want %q", axis, got, kind)
		}
	}
	if got := axisKind("communityaxis"); got != "" {
		t.Fatalf("unknown axis should map to no kind, got %q", got)
	}
}

func TestRunCapabilitiesFiltersByKindOrAxis(t *testing.T) {
	for _, filter := range []string{"state", "state-backend"} {
		var out strings.Builder
		if err := runCapabilities([]string{filter}, &out); err != nil {
			t.Fatalf("filter %q: %v", filter, err)
		}
		s := out.String()
		if !strings.Contains(s, "rat://state/v1/get") || strings.Contains(s, "rat://engine/") {
			t.Fatalf("filter %q rendered wrong axes:\n%s", filter, s)
		}
		if !strings.Contains(s, "kind: state-backend") || !strings.Contains(s, "state/v1/CONTRACT.md") {
			t.Fatalf("filter %q missing the kind/CONTRACT pointers:\n%s", filter, s)
		}
	}
}

func TestRunCapabilitiesDeploymentRuntimeWart(t *testing.T) {
	// The kind filter must reach the hyphenated URI axis, and the CONTRACT.md pointer
	// must use the hyphen-less proto dir.
	var out strings.Builder
	if err := runCapabilities([]string{"deployment-runtime"}, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "rat://deployment-runtime/v1/launch") ||
		!strings.Contains(s, "contracts/proto/rat/deploymentruntime/v1/CONTRACT.md") {
		t.Fatalf("wire-wart handling wrong:\n%s", s)
	}
}

func TestRunCapabilitiesUnknownAxis(t *testing.T) {
	var out strings.Builder
	err := runCapabilities([]string{"nope"}, &out)
	if err == nil || !strings.Contains(err.Error(), `no axis "nope"`) {
		t.Fatalf("want the no-axis error with suggestions, got %v", err)
	}
}
