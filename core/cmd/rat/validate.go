package main

// validate.go — `rat validate`: the static PREFLIGHT for a plane/project (backlog DX-1).
//
// Before this, a typo'd launch image silently backoff-retried to Degraded ~15s after
// boot while serve proceeded with N<desired plugins, and an unsatisfied `requires`
// only warned (resolver.go) until a call returned NOT_FOUND. `rat validate` runs every
// static check the daemon's boot path knows about — WITHOUT booting anything — and
// exits non-zero on findings that would leave the plane degraded:
//
//	rat validate                      # the enclosing project's rat.toml, else ./plane.yaml
//	rat validate --plane plugins.yaml # an explicit plane file
//	rat serve --strict / rat up --strict   # same checks, refusing to boot on any error
//
// Checks (all static; attach endpoints are NOT dialed):
//   - the plane/manifests load at all (LoadPlane/LoadProject — structure, names,
//     launch/endpoint exclusivity, isolation profile, capability-URI grammar)
//   - one mode per plane (assemble's launch-xor-attach rule, surfaced pre-boot)
//   - plugin names are unique
//   - every capability URI is REAL — same semantics as `rat plugin check`: hard-fail a
//     capability in an axis this rat links, merely note one in an axis it can't see
//   - a provider's `provides` stays on its kind's axis
//   - every `requires` has a provider in the plane (else the gateway has no route)
//   - every launch image is launchable (local: executable exists; podman: present
//     locally, or remotely resolvable — `podman run` pulls at launch, ADR-052)
//   - launched providers declare resources.requests (C4) — warning, not error: the
//     authoring gate mandates it, the runtime currently launches unbounded without it

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/squat-collective/rat-v3/core/manifest"
)

// vIssue is one preflight finding. Error-level issues make `rat validate` exit non-zero
// and `--strict` refuse to boot; warnings don't.
type vIssue struct {
	err bool
	msg string
}

// imageProbe answers "could the runtime launch this image, as things stand?".
// Returns (note, err): err = unlaunchable; a non-empty note = launchable with a caveat
// worth surfacing (e.g. "will be pulled at launch"). Injectable so tests don't need podman.
type imageProbe func(runtime, image string) (string, error)

// defaultImageProbe mirrors what each deployment-runtime will actually do at launch.
func defaultImageProbe(runtime, image string) (string, error) {
	switch runtime {
	case "local":
		fi, err := os.Stat(image)
		if err != nil {
			return "", fmt.Errorf("binary %q not found (the local runtime execs a filesystem path)", image)
		}
		if fi.IsDir() || fi.Mode()&0o111 == 0 {
			return "", fmt.Errorf("%q is not an executable binary", image)
		}
	case "podman":
		bin := os.Getenv("RAT_PODMAN_BIN")
		if bin == "" {
			bin = "podman"
		}
		if exec.Command(bin, "image", "exists", image).Run() == nil {
			return "", nil
		}
		// Not local — fine by itself: the runtime launches via `podman run`, which
		// auto-pulls (ADR-052; this probe's old "does not pull" claim was wrong). But a
		// typo'd ref would still crash-loop to Degraded at launch, so verify the ref
		// RESOLVES remotely (manifest inspect — no pull, no layers).
		if exec.Command(bin, "manifest", "inspect", image).Run() == nil {
			return "not local — will be pulled at launch", nil
		}
		return "", fmt.Errorf("image %q is neither local nor remotely resolvable (`%s manifest inspect` failed) — a typo'd ref crash-loops at launch", image, bin)
	}
	return "", nil
}

// preflight runs every static check against an already-loaded plane and returns the
// findings. It never boots, launches, or dials anything.
func preflight(pl *Plane, probe imageProbe) []vIssue {
	var issues []vIssue
	bad := func(format string, a ...any) { issues = append(issues, vIssue{true, fmt.Sprintf(format, a...)}) }
	warn := func(format string, a ...any) { issues = append(issues, vIssue{false, fmt.Sprintf(format, a...)}) }

	// One mode per plane + unique names (assemble rejects the former at boot; surface both now).
	var hasLaunch, hasEndpoint bool
	seen := map[string]bool{}
	manifests := make([]*manifest.Manifest, 0, len(pl.Specs))
	for _, s := range pl.Specs {
		manifests = append(manifests, s.Manifest)
		name := s.Manifest.Metadata.Name
		if seen[name] {
			bad("duplicate plugin name %q — names must be unique in a plane", name)
		}
		seen[name] = true
		if s.Launch != nil {
			hasLaunch = true
		}
		if s.Endpoint != "" {
			hasEndpoint = true
		}
	}
	if hasLaunch && hasEndpoint {
		bad("plane mixes launch and attach plugins — not supported in v1 (all-launch or all-attach; register-only drivers are fine in either)")
	}

	// Capability URIs are real — `rat plugin check` semantics (hard-fail only in linked axes).
	axes := linkedAxes()
	checkCaps := func(m *manifest.Manifest, role string, caps []string) {
		for _, c := range caps {
			if !axes[capAxisOf(c)] {
				warn("%s: %s %q — axis %q isn't compiled into this rat; capability unverified", m.Metadata.Name, role, c, capAxisOf(c))
				continue
			}
			if _, _, _, err := resolveMethod(c); err != nil {
				bad("%s: %s %q is not a real capability of axis %q (typo?)", m.Metadata.Name, role, c, capAxisOf(c))
			}
		}
	}
	for _, m := range manifests {
		checkCaps(m, "provides", m.ProvidesCaps())
		checkCaps(m, "requires", m.RequiresCaps())
		if want := kindAxis(m.Kind); want != "" {
			for _, c := range m.ProvidesCaps() {
				if a := capAxisOf(c); a != want {
					bad("%s: a %q plugin provides %q (axis %q) — expected %q-axis capabilities", m.Metadata.Name, m.Kind, c, a, want)
				}
			}
		}
	}

	// Every requires has a provider — else the gateway has no route for it.
	for _, d := range unsatisfiedRequires(manifests) {
		bad("%s requires %s — no provider in this plane (add a %s-axis plugin, or register one live later and drop --strict)", d.Plugin, d.Capability, capAxisOf(d.Capability))
	}

	// Launch images are launchable NOW + providers are resource-bounded (C4).
	for _, s := range pl.Specs {
		if s.Launch == nil {
			continue
		}
		if note, err := probe(pl.Runtime, s.Launch.Image); err != nil {
			bad("%s: launch %v", s.Manifest.Metadata.Name, err)
		} else if note != "" {
			warn("%s: launch image %q %s", s.Manifest.Metadata.Name, s.Launch.Image, note)
		}
		if !s.Manifest.HasResources() {
			warn("%s: launched provider declares no resources.requests (C4 — mandatory at the authoring gate; the runtime launches it unbounded)", s.Manifest.Metadata.Name)
		}
	}
	return issues
}

// renderPreflight prints the findings (errors first is NOT imposed — insertion order
// groups them by check) and the verdict line. Returns the error count.
func renderPreflight(out io.Writer, pl *Plane, src string, issues []vIssue) int {
	var launchN, attachN, driverN int
	for _, s := range pl.Specs {
		switch {
		case s.Launch != nil:
			launchN++
		case s.Endpoint != "":
			attachN++
		default:
			driverN++
		}
	}
	fmt.Fprintf(out, "rat validate — %s (runtime %s · %d launch · %d attach · %d driver)\n",
		src, pl.Runtime, launchN, attachN, driverN)

	errs, warns := 0, 0
	for _, is := range issues {
		if is.err {
			errs++
			fmt.Fprintf(out, "  ✗ %s\n", is.msg)
		} else {
			warns++
			fmt.Fprintf(out, "  ⚠ %s\n", is.msg)
		}
	}
	switch {
	case errs > 0:
		fmt.Fprintf(out, "✗ %d error(s), %d warning(s) — this plane will NOT come up as declared.\n", errs, warns)
	case warns > 0:
		fmt.Fprintf(out, "✓ launchable as declared — %d warning(s) above (static checks only; endpoints not dialed)\n", warns)
	default:
		fmt.Fprintf(out, "✓ launchable as declared — manifests load · names unique · capabilities real · all requires satisfied · images present (static checks only; endpoints not dialed)\n")
	}
	return errs
}

// loadForValidate resolves what to validate: an explicit --plane, else the enclosing
// project's rat.toml, else ./plane.yaml.
func loadForValidate(planePath string) (*Plane, string, error) {
	if planePath != "" {
		pl, err := LoadPlane(planePath)
		return pl, planePath, err
	}
	if tomlPath, _, err := findProject("."); err == nil {
		pl, lerr := LoadProject(tomlPath)
		return pl, tomlPath, lerr
	}
	if _, err := os.Stat("plane.yaml"); err == nil {
		pl, lerr := LoadPlane("plane.yaml")
		return pl, "plane.yaml", lerr
	}
	return nil, "", fmt.Errorf("nothing to validate: no rat.toml in . or any parent and no ./plane.yaml (use --plane <file>)")
}

// runValidate implements `rat validate [--plane <file>]`.
func runValidate(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat validate", flag.ContinueOnError)
	planePath := fs.String("plane", "", "validate this plane file (default: the project's rat.toml, else ./plane.yaml)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pl, src, err := loadForValidate(*planePath)
	if err != nil {
		// A plane that doesn't even load IS the preflight finding.
		return fmt.Errorf("preflight: %w", err)
	}
	if errs := renderPreflight(out, pl, src, preflight(pl, defaultImageProbe)); errs > 0 {
		return fmt.Errorf("%d preflight error(s)", errs)
	}
	return nil
}

// strictPreflight is the `--strict` boot gate shared by `rat serve` and `rat up`:
// run the preflight, print the findings, refuse to boot on any error.
func strictPreflight(pl *Plane, src string) error {
	if errs := renderPreflight(os.Stderr, pl, src, preflight(pl, defaultImageProbe)); errs > 0 {
		return fmt.Errorf("--strict: %d preflight error(s) — fix the plane, or boot without --strict", errs)
	}
	return nil
}
