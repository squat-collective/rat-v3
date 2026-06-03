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
	"strings"
)

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

// readSource reads a marketplace index from a file path or an http(s) URL.
func readSource(path string) ([]byte, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		resp, err := http.Get(path)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(path)
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

// addedEntries: the plugins listed by every registered marketplace index.
func addedEntries() []marketEntry {
	var out []marketEntry
	for _, src := range loadMarketConfig().Marketplaces {
		data, err := readSource(src.Path)
		if err != nil {
			continue
		}
		var idx marketIndex
		if json.Unmarshal(data, &idx) != nil {
			continue
		}
		for _, e := range idx.Plugins {
			e.source = src.Name
			out = append(out, e)
		}
	}
	return out
}

// allMarketEntries: local + added (local first, so a locally-built image wins on display).
func allMarketEntries() []marketEntry {
	return append(localEntries(), addedEntries()...)
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
	var matched []marketEntry
	for _, e := range allMarketEntries() {
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
		if len(args) < 3 {
			return fmt.Errorf("usage: rat marketplace add <name> <path-or-url>")
		}
		cfg := loadMarketConfig()
		for _, s := range cfg.Marketplaces {
			if s.Name == args[1] {
				return fmt.Errorf("marketplace %q already added", args[1])
			}
		}
		cfg.Marketplaces = append(cfg.Marketplaces, marketSource{Name: args[1], Path: args[2]})
		if err := saveMarketConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "added marketplace %q → %s\n", args[1], args[2])
		return nil
	case "list":
		cfg := loadMarketConfig()
		fmt.Fprintf(out, "marketplaces:\n  %-12s %s\n", "local", "(plugin images on this machine, via their stamped manifest)")
		for _, s := range cfg.Marketplaces {
			fmt.Fprintf(out, "  %-12s %s\n", s.Name, s.Path)
		}
		return nil
	default:
		return fmt.Errorf("usage: rat marketplace <add|list>")
	}
}

// reportUnsatisfiedSuggesting is `rat add`'s resolver report, enhanced with marketplace
// auto-suggestions: for each unsatisfied requires, name the exact plugin (+ `rat add`) that
// provides it, if any marketplace has one.
func reportUnsatisfiedSuggesting(out io.Writer, miss []missingDep) {
	if len(miss) == 0 {
		return
	}
	entries := allMarketEntries()
	fmt.Fprintf(out, "⚠ %d unsatisfied dependenc%s:\n", len(miss), plural(len(miss)))
	for _, d := range miss {
		if e, ok := providerFor(entries, d.Capability); ok {
			fmt.Fprintf(out, "   %s requires %s → rat add --image %s  (%s, from %s)\n", d.Plugin, d.Capability, e.Image, e.Name, e.source)
		} else {
			fmt.Fprintf(out, "   %s requires %s — no marketplace provider (add a %s-axis plugin)\n", d.Plugin, d.Capability, capAxisOf(d.Capability))
		}
	}
}
