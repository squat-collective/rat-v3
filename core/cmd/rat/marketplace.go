package main

// marketplace.go — plugin discovery (the `kind: marketplace` axis, ADR-001 / the inbox
// distribution+marketplace idea). A marketplace is a SOURCE of plugin entries; rat reads
// several at once: the LOCAL one (plugin images on this machine, found by their stamped
// manifest, ADR-026) plus ADDED ones (index files / URLs the operator registered). Verbs:
//   rat search [query]            — find plugins across local + added marketplaces
//   rat list                      — the plugins installed in THIS project (rat.toml)
//   rat marketplace add <name> <source> | list  — manage the added marketplaces
// And `rat add`'s satisfiability resolver uses the index to suggest the EXACT plugin to add.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// officialIndexURL is the canonical home of the reference RAT marketplace — `rat
// marketplace add official` (no URL) registers it. It tracks marketplace/rat-official.json
// in this repo, published as a static file (GitHub Pages for the rat-dev org). Placeholder
// host until the org's Pages site is live; the file format is what's load-bearing.
const officialIndexURL = "https://rat-dev.github.io/marketplace/official.json"

// wellKnownMarketplaces maps a short name → its canonical URL, so `rat marketplace add
// <name>` needs no URL for the built-ins.
var wellKnownMarketplaces = map[string]string{"official": officialIndexURL}

func wellKnownNames() []string {
	names := make([]string, 0, len(wellKnownMarketplaces))
	for n := range wellKnownMarketplaces {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// marketEntry is one plugin a marketplace advertises.
type marketEntry struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Image       string   `json:"image"`
	Version     string   `json:"version"`
	Provides    []string `json:"provides"`
	Requires    []string `json:"requires"`
	Description string   `json:"description"`
	source      string   // the marketplace it came from (not serialized)
}

type marketIndex struct {
	Name    string        `json:"name"`
	Plugins []marketEntry `json:"plugins"`
}

type marketSource struct {
	Name string `json:"name"`
	Path string `json:"path"` // a file path or an http(s) URL
}

type marketConfig struct {
	Marketplaces []marketSource `json:"marketplaces"`
}

func marketConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "rat", "marketplaces.json")
}

func loadMarketConfig() marketConfig {
	var cfg marketConfig
	if data, err := os.ReadFile(marketConfigPath()); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	return cfg
}

func saveMarketConfig(cfg marketConfig) error {
	p := marketConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(p, data, 0o644)
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// marketCacheDir holds the last-good copy of each fetched remote index, so `rat search`
// keeps working offline once an index has been seen.
func marketCacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "rat", "marketplaces")
}

// marketHTTP has a bounded timeout so a hung host can't wedge every `rat search`.
var marketHTTP = &http.Client{Timeout: 10 * time.Second}

// fetchSource loads a marketplace index. File paths read directly. URLs are fetched (with a
// timeout) and cached to disk; on a fetch failure the last cached copy is used as a fallback
// (returned with a `note`), so a remote index degrades gracefully offline.
func fetchSource(src marketSource) (data []byte, note string, err error) {
	if !isURL(src.Path) {
		b, e := os.ReadFile(src.Path)
		return b, "", e
	}
	cache := filepath.Join(marketCacheDir(), src.Name+".json")
	resp, e := marketHTTP.Get(src.Path)
	if e == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			if b, re := io.ReadAll(resp.Body); re == nil {
				_ = os.MkdirAll(marketCacheDir(), 0o755)
				_ = os.WriteFile(cache, b, 0o644)
				return b, "", nil
			} else {
				e = re
			}
		} else {
			e = fmt.Errorf("HTTP %s", resp.Status)
		}
	}
	if b, ce := os.ReadFile(cache); ce == nil { // fetch failed → fall back to cache
		return b, fmt.Sprintf("⚠ %s unreachable (%v) — using cached copy", src.Name, e), nil
	}
	return nil, "", fmt.Errorf("fetch %s: %w", src.Path, e)
}

// localEntries: plugin IMAGES on this machine, found by their stamped manifest (ADR-026).
func localEntries() []marketEntry {
	out := []marketEntry{}
	got, err := exec.Command("podman", "images", "--filter", "label="+manifestLabel, "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return out
	}
	for _, ref := range strings.Fields(string(got)) {
		if ref == "" || strings.Contains(ref, "<none>") {
			continue
		}
		m, _, err := readStampedManifest(ref)
		if err != nil {
			continue
		}
		out = append(out, marketEntry{
			Name: m.Metadata.Name, Kind: m.Kind, Image: ref, Version: m.Metadata.Version,
			Provides: m.ProvidesCaps(), Requires: m.RequiresCaps(), source: "local",
		})
	}
	return out
}

// addedEntries: the plugins listed by every registered marketplace index, plus any warnings
// (an unreachable remote, a malformed index) so callers can surface a degraded source instead
// of silently dropping it.
func addedEntries() (entries []marketEntry, warns []string) {
	for _, src := range loadMarketConfig().Marketplaces {
		data, note, err := fetchSource(src)
		if err != nil {
			warns = append(warns, fmt.Sprintf("⚠ marketplace %q: %v", src.Name, err))
			continue
		}
		if note != "" {
			warns = append(warns, note)
		}
		var idx marketIndex
		if json.Unmarshal(data, &idx) != nil {
			warns = append(warns, fmt.Sprintf("⚠ marketplace %q: malformed index", src.Name))
			continue
		}
		for _, e := range idx.Plugins {
			e.source = src.Name
			entries = append(entries, e)
		}
	}
	return entries, warns
}

// allMarketEntries: local + added (local first, so a locally-built image wins on display),
// plus the added sources' warnings.
func allMarketEntries() (entries []marketEntry, warns []string) {
	added, warns := addedEntries()
	return append(localEntries(), added...), warns
}

// providerFor returns the first marketplace entry that provides a capability (the auto-suggest).
func providerFor(entries []marketEntry, capability string) (marketEntry, bool) {
	for _, e := range entries {
		for _, p := range e.Provides {
			if p == capability {
				return e, true
			}
		}
	}
	return marketEntry{}, false
}

// --- verbs -----------------------------------------------------------------------------

func runSearch(args []string, out io.Writer) error {
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query = strings.ToLower(args[0])
	}
	entries, warns := allMarketEntries()
	for _, w := range warns {
		fmt.Fprintln(out, w)
	}
	var matched []marketEntry
	for _, e := range entries {
		if query == "" || matchesQuery(e, query) {
			matched = append(matched, e)
		}
	}
	if len(matched) == 0 {
		fmt.Fprintf(out, "no plugins match %q (try `rat marketplace add` a marketplace, or `rat plugin pack` a local one)\n", query)
		return nil
	}
	fmt.Fprintf(out, "%-16s %-18s %-9s %s\n", "NAME", "KIND", "SOURCE", "PROVIDES")
	for _, e := range matched {
		fmt.Fprintf(out, "%-16s %-18s %-9s %s\n", e.Name, e.Kind, e.source, strings.Join(e.Provides, ", "))
	}
	return nil
}

func matchesQuery(e marketEntry, q string) bool {
	if strings.Contains(strings.ToLower(e.Name), q) ||
		strings.Contains(strings.ToLower(e.Kind), q) ||
		strings.Contains(strings.ToLower(e.Description), q) {
		return true
	}
	for _, c := range append(e.Provides, e.Requires...) { // search by capability too
		if strings.Contains(strings.ToLower(c), q) {
			return true
		}
	}
	return false
}

func runList(args []string, out io.Writer) error {
	tomlPath, _, err := findProject(".")
	if err != nil {
		return err
	}
	rt, err := parseProject(tomlPath)
	if err != nil {
		return err
	}
	if len(rt.Plugins) == 0 {
		fmt.Fprintf(out, "no plugins in this project — `rat add` one (`rat search` to find them)\n")
		return nil
	}
	fmt.Fprintf(out, "%d plugin(s) in %s:\n", len(rt.Plugins), projectFile)
	for _, p := range rt.Plugins {
		img := p.Image
		if img == "" {
			img = "(driver)"
		}
		fmt.Fprintf(out, "  %-18s %s\n", p.Name, img)
	}
	return nil
}

func runMarketplace(args []string, out io.Writer) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: rat marketplace add <name> [<path-or-url>]  (URL optional for a built-in like `official`)")
		}
		name := args[1]
		path := ""
		if len(args) >= 3 {
			path = args[2]
		} else if u, ok := wellKnownMarketplaces[name]; ok {
			path = u // built-in: `rat marketplace add official` needs no URL
		} else {
			return fmt.Errorf("usage: rat marketplace add %s <path-or-url>  (or add a built-in: %s)", name, strings.Join(wellKnownNames(), ", "))
		}
		cfg := loadMarketConfig()
		for _, s := range cfg.Marketplaces {
			if s.Name == name {
				return fmt.Errorf("marketplace %q already added", name)
			}
		}
		cfg.Marketplaces = append(cfg.Marketplaces, marketSource{Name: name, Path: path})
		if err := saveMarketConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "added marketplace %q → %s\n", name, path)
		return nil
	case "list":
		cfg := loadMarketConfig()
		fmt.Fprintf(out, "marketplaces:\n  %-12s %s\n", "local", "(plugin images on this machine, via their stamped manifest)")
		added := map[string]bool{}
		for _, s := range cfg.Marketplaces {
			kind := "(file)"
			if isURL(s.Path) {
				kind = "(remote)"
			}
			fmt.Fprintf(out, "  %-12s %-8s %s\n", s.Name, kind, s.Path)
			added[s.Name] = true
		}
		// surface built-ins not yet added, so `official` is discoverable.
		for _, n := range wellKnownNames() {
			if !added[n] {
				fmt.Fprintf(out, "  %-12s %-8s %s  (add with `rat marketplace add %s`)\n", n, "(built-in)", wellKnownMarketplaces[n], n)
			}
		}
		return nil
	default:
		return fmt.Errorf("usage: rat marketplace <add|list>")
	}
}

// addMarketEntry adds a marketplace provider to the project (the `rat add --with-deps`
// machinery). It SYNTHESIZES a manifest from the entry's advertised kind/provides/requires —
// no image pull at declare-time (the image is fetched at `rat up`/`serve`) — writes
// manifests/<name>.plugin.yaml, and appends a [[plugin]] block. Returns false (no error) if a
// plugin of that name is already present, so the resolver loop converges.
func addMarketEntry(out io.Writer, tomlPath, dir string, e marketEntry) (bool, error) {
	rt, err := parseProject(tomlPath)
	if err != nil {
		return false, err
	}
	for _, p := range rt.Plugins {
		if p.Name == e.Name {
			return false, nil
		}
	}
	rel := filepath.Join("manifests", e.Name+".plugin.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "manifests"), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(dir, rel), synthManifest(e), 0o644); err != nil {
		return false, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n[[plugin]]\nname     = %q\n", e.Name)
	if e.Image != "" {
		fmt.Fprintf(&b, "image    = %q\n", e.Image)
	}
	fmt.Fprintf(&b, "manifest = %q\n", rel)
	if e.Image != "" {
		fmt.Fprintf(&b, "isolation = %q\n", "i9")
	}
	f, err := os.OpenFile(tomlPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return false, err
	}
	fmt.Fprintf(out, "  + %s (%s, from %s) — provides %s\n", e.Name, e.Image, e.source, strings.Join(e.Provides, ", "))
	return true, nil
}

// synthManifest renders a marketplace entry as a plugin manifest (the frozen YAML subset the
// loader reads). The entry's provides/requires ARE the manifest's — that's how a marketplace
// index doubles as a dependency declaration.
func synthManifest(e marketEntry) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# synthesized by `rat add --with-deps` from marketplace %q\n", e.source)
	b.WriteString("api_version: rat.dev/v1\n")
	fmt.Fprintf(&b, "kind: %s\n", e.Kind)
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  name: %s\n", e.Name)
	ver := e.Version
	if ver == "" {
		ver = "0.0"
	}
	fmt.Fprintf(&b, "  version: %q\n", ver)
	if len(e.Provides) > 0 {
		b.WriteString("provides:\n")
		for _, c := range e.Provides {
			fmt.Fprintf(&b, "  - capability: %s\n", c)
		}
	}
	if len(e.Requires) > 0 {
		b.WriteString("requires:\n")
		for _, c := range e.Requires {
			fmt.Fprintf(&b, "  - capability: %s\n", c)
		}
	}
	return []byte(b.String())
}

// reportUnsatisfiedSuggesting is `rat add`'s resolver report, enhanced with marketplace
// auto-suggestions: for each unsatisfied requires, name the exact plugin (+ `rat add`) that
// provides it, if any marketplace has one.
func reportUnsatisfiedSuggesting(out io.Writer, miss []missingDep) {
	if len(miss) == 0 {
		return
	}
	entries, _ := allMarketEntries()
	fmt.Fprintf(out, "⚠ %d unsatisfied dependenc%s:\n", len(miss), plural(len(miss)))
	for _, d := range miss {
		if e, ok := providerFor(entries, d.Capability); ok {
			fmt.Fprintf(out, "   %s requires %s → rat add --image %s  (%s, from %s)\n", d.Plugin, d.Capability, e.Image, e.Name, e.source)
		} else {
			fmt.Fprintf(out, "   %s requires %s — no marketplace provider (add a %s-axis plugin)\n", d.Plugin, d.Capability, capAxisOf(d.Capability))
		}
	}
}
