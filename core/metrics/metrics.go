// Package metrics is the core's NATIVE, dependency-free observability surface
// (plugin-architecture.md "native observability" — a correctness condition of the six things,
// not the observability-axis plugin). It is a tiny Prometheus text-exposition registry the
// daemon serves at /metrics, so a `rat serve` is observable with NO plugin installed; an
// observability plugin layers richer telemetry on top, it is not required for the basics.
//
// Two metric shapes, matching how the core produces them:
//   - cumulative COUNTERS, pushed via Inc/Add as events happen (e.g. gateway call outcomes);
//   - GAUGES collected at scrape via a registered func (the pull model — e.g. live plugin
//     states read from the reconciler when /metrics is hit).
//
// No third-party dependency: the Prometheus text format is small, and the six-thing-core
// discipline says don't pull a library in for what is ~100 lines of stable format.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// Sample is one labeled value produced by a gauge collection.
type Sample struct {
	Labels map[string]string
	Value  float64
}

type counterVec struct {
	help   string
	values map[string]float64           // labelKey -> cumulative value
	labels map[string]map[string]string // labelKey -> the label set (for rendering)
}

type gaugeFunc struct {
	help string
	fn   func() []Sample
}

// Registry holds the core's counters + gauge collectors. Safe for concurrent use.
type Registry struct {
	mu       sync.Mutex
	counters map[string]*counterVec
	gauges   map[string]*gaugeFunc
	order    []string // gauge registration order (stable output)
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{counters: map[string]*counterVec{}, gauges: map[string]*gaugeFunc{}}
}

// Inc adds 1 to counter `name` for the label set (registering it with `help` on first use).
func (r *Registry) Inc(name, help string, labels map[string]string) { r.Add(name, help, labels, 1) }

// Add adds delta to counter `name` for the label set.
func (r *Registry) Add(name, help string, labels map[string]string, delta float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cv := r.counters[name]
	if cv == nil {
		cv = &counterVec{help: help, values: map[string]float64{}, labels: map[string]map[string]string{}}
		r.counters[name] = cv
	}
	key := labelKey(labels)
	cv.values[key] += delta
	if _, ok := cv.labels[key]; !ok {
		cv.labels[key] = labels
	}
}

// RegisterGaugeFunc registers a gauge collected at scrape time (e.g. live plugin states). The
// func is called on each WritePrometheus, NOT while the registry lock is held (so it may take
// other locks safely). Idempotent by name.
func (r *Registry) RegisterGaugeFunc(name, help string, fn func() []Sample) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.gauges[name]; !ok {
		r.order = append(r.order, name)
	}
	r.gauges[name] = &gaugeFunc{help: help, fn: fn}
}

// WritePrometheus renders the registry in Prometheus text-exposition format.
func (r *Registry) WritePrometheus(w io.Writer) {
	// Snapshot under the lock; render (and call gauge funcs) after releasing it.
	r.mu.Lock()
	cNames := make([]string, 0, len(r.counters))
	for n := range r.counters {
		cNames = append(cNames, n)
	}
	sort.Strings(cNames)
	type cline struct {
		labels map[string]string
		value  float64
	}
	counters := make(map[string][]cline, len(cNames))
	cHelp := make(map[string]string, len(cNames))
	for _, n := range cNames {
		cv := r.counters[n]
		cHelp[n] = cv.help
		keys := make([]string, 0, len(cv.values))
		for k := range cv.values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			counters[n] = append(counters[n], cline{labels: cv.labels[k], value: cv.values[k]})
		}
	}
	gNames := append([]string(nil), r.order...)
	gFns := make(map[string]*gaugeFunc, len(gNames))
	for _, n := range gNames {
		gFns[n] = r.gauges[n]
	}
	r.mu.Unlock()

	for _, n := range cNames {
		if h := cHelp[n]; h != "" {
			fmt.Fprintf(w, "# HELP %s %s\n", n, h)
		}
		fmt.Fprintf(w, "# TYPE %s counter\n", n)
		for _, ln := range counters[n] {
			fmt.Fprintf(w, "%s%s %g\n", n, renderLabels(ln.labels), ln.value)
		}
	}
	for _, n := range gNames {
		gf := gFns[n]
		if gf.help != "" {
			fmt.Fprintf(w, "# HELP %s %s\n", n, gf.help)
		}
		fmt.Fprintf(w, "# TYPE %s gauge\n", n)
		for _, s := range gf.fn() {
			fmt.Fprintf(w, "%s%s %g\n", n, renderLabels(s.Labels), s.Value)
		}
	}
}

// labelKey is a deterministic key for a label set (sorted k=v), used as the counter map key.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
		b.WriteByte('\x00')
	}
	return b.String()
}

// renderLabels renders a label set as Prometheus `{k="v",k="v"}` (sorted), "" if empty. Label
// values are escaped per the exposition format (\\, \n, \").
func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+`="`+escapeValue(labels[k])+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return v
}
