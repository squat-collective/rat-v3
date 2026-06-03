package main

// project.go — the poetry-style project layer (ADR-023). A project is a directory with a
// `rat.toml` (the committed spec). The verbs `rat init` / `rat add` / `rat up` write and
// run that spec; the daemon stays config-stateless (it just reads rat.toml). rat.toml is
// COMMAND-WRITTEN, never hand-edited — exactly poetry's pyproject.toml model.
//
//	name    = "my-project"              # instance id (namespaces runtime resources)
//	runtime = "podman"                  # local | podman
//	addr    = "unix:.rat/daemon.sock"   # default: a per-project unix socket (no port war)
//
//	[[plugin]]
//	name     = "rat-state"
//	image    = "rat/state:dev"
//	manifest = "manifests/state.plugin.yaml"
//	isolation = "i9"
//	[plugin.env]
//	RAT_STATE_PG_REF = "ref://state/pg-dsn"

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	manifestpkg "github.com/rat-dev/rat/core/manifest"
	toml "github.com/pelletier/go-toml/v2"
)

const projectFile = "rat.toml"

// ratToml / ratTomlPlugin mirror rat.toml on disk; they reduce to rawPlane so the YAML
// plane and the TOML project share one validation/bring-up path (planeFromRaw).
type ratToml struct {
	Name          string          `toml:"name"`
	Runtime       string          `toml:"runtime"`
	Addr          string          `toml:"addr"`
	HealthTimeout string          `toml:"health_timeout"`
	Plugins       []ratTomlPlugin `toml:"plugin"`
}

type ratTomlPlugin struct {
	Name      string            `toml:"name"`
	Image     string            `toml:"image"`
	Manifest  string            `toml:"manifest"`
	Endpoint  string            `toml:"endpoint"`
	Isolation string            `toml:"isolation"`
	Env       map[string]string `toml:"env"`
}

// findProject walks up from start to locate the nearest rat.toml (like git/poetry/cargo),
// returning its path + the project directory. Not-found is a clear, actionable error.
func findProject(start string) (tomlPath, dir string, err error) {
	d, err := filepath.Abs(start)
	if err != nil {
		return "", "", err
	}
	for {
		p := filepath.Join(d, projectFile)
		if _, err := os.Stat(p); err == nil {
			return p, d, nil
		}
		parent := filepath.Dir(d)
		if parent == d { // reached the filesystem root
			return "", "", fmt.Errorf("no %s found in %s or any parent (run `rat init`)", projectFile, start)
		}
		d = parent
	}
}

// parseProject reads + unmarshals a rat.toml.
func parseProject(tomlPath string) (*ratToml, error) {
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", tomlPath, err)
	}
	var rt ratToml
	if err := toml.Unmarshal(data, &rt); err != nil {
		return nil, fmt.Errorf("parse %s: %w", tomlPath, err)
	}
	return &rt, nil
}

// LoadProject parses rat.toml and reduces it to a ready-to-bring-up Plane. The default
// control address is this project's per-project unix socket (.rat/daemon.sock under the
// project dir) — the ADR-023 default that lets many rats coexist.
func LoadProject(tomlPath string) (*Plane, error) {
	rt, err := parseProject(tomlPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(tomlPath)
	rp := &rawPlane{
		Name:          rt.Name,
		Runtime:       rt.Runtime,
		Addr:          rt.Addr,
		HealthTimeout: rt.HealthTimeout,
	}
	if rp.Addr == "" {
		rp.Addr = "unix:" + filepath.Join(dir, ".rat", "daemon.sock")
	}
	for _, p := range rt.Plugins {
		rpl := rawPlugin{Name: p.Name, Manifest: p.Manifest, Endpoint: p.Endpoint}
		if p.Image != "" {
			rpl.Launch = &rawLaunch{Image: p.Image, Isolation: p.Isolation, Env: p.Env}
		}
		rp.Plugins = append(rp.Plugins, rpl)
	}
	pl, err := planeFromRaw(rp, tomlPath)
	if err != nil {
		return nil, err
	}
	pl.RuntimeDir = filepath.Join(dir, ".rat") // the daemon registers itself here (slice 2c)
	return pl, nil
}

// --- verbs -----------------------------------------------------------------------------

// runInit writes a starter rat.toml in the current directory (poetry init): the project
// SHELL only — name + runtime + the per-project socket default, NO plugins (those arrive
// via `rat add`). It refuses to clobber an existing project.
func runInit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat init", flag.ContinueOnError)
	name := fs.String("name", "", "project name (default: current directory name)")
	runtime := fs.String("runtime", "local", "deployment runtime: local | podman")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(projectFile); err == nil {
		return fmt.Errorf("%s already exists here", projectFile)
	}
	cwd, _ := os.Getwd()
	pname := orDefault(*name, instanceID(filepath.Base(cwd)))
	body := fmt.Sprintf(`# rat.toml — your project's plugin set (ADR-023). Command-written; edit with
# `+"`rat add`"+`/`+"`rat remove`"+`, run with `+"`rat up`"+`. Commit this file + rat.lock; .rat/ is runtime.
name    = %q
runtime = %q
# addr defaults to a per-project unix socket (.rat/daemon.sock) — many rats coexist.

# Add plugins with: rat add <name> --image <ref> --manifest <path>
`, pname, *runtime)
	if err := os.WriteFile(projectFile, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", projectFile, err)
	}
	// .rat/ holds runtime junk (socket, pid, logs) — gitignored, like .venv.
	_ = os.MkdirAll(".rat", 0o755)
	_ = os.WriteFile(filepath.Join(".rat", ".gitignore"), []byte("*\n"), 0o644)
	fmt.Fprintf(out, "initialized %s (project %q, runtime %s)\n", projectFile, pname, *runtime)
	return nil
}

// runAdd records a plugin in rat.toml (poetry add). It APPENDS a [[plugin]] block (so the
// file's comments + ordering survive), after a duplicate-name check. A plugin with an
// --image is launched; without one it is a register-only driver (an operator identity). A
// live daemon is NOT hot-registered yet — `rat up` materializes the change (the live
// `rat add` path lands with the RegisterPlugin RPC, ADR-023).
func runAdd(args []string, out io.Writer) error {
	// Pull the leading positional <name> off first so the poetry-shaped `rat add <name>
	// --flags…` works (Go's flag package otherwise stops parsing at the first positional).
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("rat add", flag.ContinueOnError)
	image := fs.String("image", "", "plugin OCI image ref (omit for a register-only driver)")
	manifest := fs.String("manifest", "", "path to the plugin manifest (required)")
	isolation := fs.String("isolation", "i9", "isolation profile")
	withDeps := fs.Bool("with-deps", false, "auto-add marketplace providers for any unsatisfied `requires` (transitive)")
	requireSigned := fs.Bool("require-signed", false, "with --with-deps, only auto-add providers from signature-verified marketplaces")
	var envs multiFlag
	fs.Var(&envs, "env", "env var KEY=VALUE (repeatable; NEVER secrets)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tomlPath, dir, err := findProject(".")
	if err != nil {
		return err
	}

	// Manifest-from-image (ADR-026 Q05): with --image and no --manifest, read the manifest
	// STAMPED into the (packed) image, materialize it into the project, and reference it. So
	// `rat add --image <packed-ref>` needs no --manifest, and the name comes from the manifest.
	if *manifest == "" && *image != "" {
		if err := ensureImagePresent(out, *image); err != nil {
			return err
		}
		m, raw, err := readStampedManifest(*image)
		if err != nil {
			return fmt.Errorf("%s: %w (pass --manifest, or `rat plugin pack` the image to stamp one in)", *image, err)
		}
		if name == "" {
			name = m.Metadata.Name
		}
		rel := filepath.Join("manifests", name+".plugin.yaml")
		if err := os.MkdirAll(filepath.Join(dir, "manifests"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, rel), raw, 0o644); err != nil {
			return err
		}
		*manifest = rel
		fmt.Fprintf(out, "read manifest from %s → %s\n", *image, rel)
	}
	if name == "" {
		return fmt.Errorf("usage: rat add <name> --manifest <path> [--image <ref>]  (or `rat add --image <packed-ref>` to read the stamped manifest)")
	}
	if *manifest == "" {
		return fmt.Errorf("--manifest <path> required (or pass --image a packed image whose manifest is stamped in)")
	}

	rt, err := parseProject(tomlPath)
	if err != nil {
		return err
	}
	for _, p := range rt.Plugins {
		if p.Name == name {
			return fmt.Errorf("plugin %q is already in %s", name, projectFile)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n[[plugin]]\nname     = %q\n", name)
	if *image != "" {
		fmt.Fprintf(&b, "image    = %q\n", *image)
	}
	fmt.Fprintf(&b, "manifest = %q\n", *manifest)
	if *image != "" {
		fmt.Fprintf(&b, "isolation = %q\n", *isolation)
	}
	if len(envs) > 0 {
		fmt.Fprintf(&b, "[plugin.env]\n")
		for _, kv := range envs {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return fmt.Errorf("--env %q must be KEY=VALUE", kv)
			}
			fmt.Fprintf(&b, "%s = %q\n", k, v)
		}
	}
	f, err := os.OpenFile(tomlPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", projectFile, err)
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return fmt.Errorf("append to %s: %w", projectFile, err)
	}
	kind := "driver"
	if *image != "" {
		kind = *image
	}
	fmt.Fprintf(out, "added %q (%s) to %s\n", name, kind, projectFile)

	// poetry-style: after adding, resolve the project's `requires`.
	//   --with-deps  → auto-add the marketplace provider for each (transitively).
	//   otherwise    → just surface what's now unsatisfied, with a suggestion.
	if *withDeps {
		return resolveWithDeps(out, tomlPath, dir, *requireSigned)
	}
	if pl, err := LoadProject(tomlPath); err == nil {
		reportUnsatisfiedSuggesting(out, unsatisfiedRequires(manifestsOf(pl)))
	}
	return nil
}

// manifestsOf flattens a loaded plane's specs to their manifests (the resolver's input).
func manifestsOf(pl *Plane) []*manifestpkg.Manifest {
	ms := make([]*manifestpkg.Manifest, 0, len(pl.Specs))
	for _, s := range pl.Specs {
		ms = append(ms, s.Manifest)
	}
	return ms
}

// resolveWithDeps repeatedly adds marketplace providers for the project's unsatisfied
// `requires` until every one has a provider in the project — or no marketplace can supply
// one. Transitive: a provider added this round has its OWN `requires` resolved next round
// (e.g. add rat-scheduler → pulls rat-state + dbt-runner → rat-state pulls rat-secret).
func resolveWithDeps(out io.Writer, tomlPath, dir string, requireSigned bool) error {
	entries, warns := allMarketEntries()
	for _, w := range warns {
		fmt.Fprintln(out, w)
	}
	for {
		pl, err := LoadProject(tomlPath)
		if err != nil {
			return err
		}
		miss := unsatisfiedRequires(manifestsOf(pl))
		if len(miss) == 0 {
			fmt.Fprintf(out, "✓ all dependencies satisfied\n")
			return nil
		}
		progress := false
		for _, d := range miss {
			e, ok := providerFor(entries, d.Capability)
			if !ok {
				continue
			}
			if requireSigned && !e.verified {
				// --require-signed: refuse to auto-pull an unverified provider; leave the
				// dep unsatisfied so it surfaces in the final report.
				fmt.Fprintf(out, "  ✗ skipping %s for %s — source %q is unsigned (--require-signed)\n", e.Name, d.Capability, e.source)
				continue
			}
			added, err := addMarketEntry(out, tomlPath, dir, e)
			if err != nil {
				return err
			}
			progress = progress || added
		}
		if !progress {
			// what's left has no (acceptable) marketplace provider — report it.
			reportUnsatisfiedSuggesting(out, miss)
			return nil
		}
	}
}

// multiFlag collects a repeatable string flag (e.g. --env A=1 --env B=2).
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
