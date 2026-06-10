package metrics

import (
	"strings"
	"testing"
)

// TestCountersAndGaugeFunc: counters accumulate per label set and a gauge func is collected at
// render, all emitted in valid Prometheus text exposition (HELP/TYPE + stable, sorted samples).
func TestCountersAndGaugeFunc(t *testing.T) {
	r := NewRegistry()
	r.Inc("rat_gateway_calls_total", "calls by outcome", map[string]string{"capability": "rat://state/v1/get", "outcome": "allow"})
	r.Inc("rat_gateway_calls_total", "calls by outcome", map[string]string{"capability": "rat://state/v1/get", "outcome": "allow"})
	r.Inc("rat_gateway_calls_total", "calls by outcome", map[string]string{"capability": "rat://state/v1/put", "outcome": "permission_denied"})

	r.RegisterGaugeFunc("rat_plugin_up", "1 if healthy", func() []Sample {
		return []Sample{
			{Labels: map[string]string{"plugin": "engine-spark"}, Value: 1},
			{Labels: map[string]string{"plugin": "engine-duckdb"}, Value: 0},
		}
	})

	var sb strings.Builder
	r.WritePrometheus(&sb)
	out := sb.String()

	for _, want := range []string{
		"# TYPE rat_gateway_calls_total counter",
		`rat_gateway_calls_total{capability="rat://state/v1/get",outcome="allow"} 2`,
		`rat_gateway_calls_total{capability="rat://state/v1/put",outcome="permission_denied"} 1`,
		"# TYPE rat_plugin_up gauge",
		`rat_plugin_up{plugin="engine-spark"} 1`,
		`rat_plugin_up{plugin="engine-duckdb"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exposition missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestNilRegistrySafe: a nil registry's mutators are no-ops (so an un-wired core never panics).
func TestNilRegistrySafe(t *testing.T) {
	var r *Registry
	r.Inc("x", "", nil)            // must not panic
	r.RegisterGaugeFunc("y", "", nil) // must not panic
}

// TestLabelValueEscaping: special chars in a label value are escaped per the exposition format.
func TestLabelValueEscaping(t *testing.T) {
	r := NewRegistry()
	r.Inc("m", "h", map[string]string{"reason": `a"b\c` + "\n"})
	var sb strings.Builder
	r.WritePrometheus(&sb)
	if !strings.Contains(sb.String(), `reason="a\"b\\c\n"`) {
		t.Errorf("label value not escaped: %s", sb.String())
	}
}
