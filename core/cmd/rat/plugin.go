package main

// plugin.go — the `rat plugin` authoring toolkit (ADR-026): build-time verbs for creating,
// validating, packaging, and publishing plugins. This prototype lands the two provable,
// immediately-useful ones: `init` (scaffold a ready-to-build plugin folder, poetry-init style)
// and `check` (the fast STATIC gate — manifest schema + per-kind + coherence). `test`/`pack`/
// `publish` (launch+conformance, verified image, GHCR) are the next slices.

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/squat-collective/rat-v3/core/deploymentruntime"
	"github.com/squat-collective/rat-v3/core/manifest"
	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	deploymentruntimev1 "github.com/squat-collective/rat-v3/gen/rat/deploymentruntime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// the 18 frozen axes (plugin-architecture.md). A plugin's kind must be one of these.
var knownKinds = map[string]bool{
	"engine": true, "runtime": true, "format": true, "strategy": true, "catalog": true,
	"storage": true, "deployment-runtime": true, "state-backend": true, "secret-backend": true,
	"scheduler-backend": true, "identity": true, "tenancy": true, "billing": true,
	"observability": true, "audit-log": true, "ui": true, "notifications": true, "marketplace": true,
}

// kindProvides scaffolds the capabilities a plugin of each kind serves (the well-known ones;
// driver kinds like scheduler-backend/ui serve none and `requires` instead).
var kindProvides = map[string][]string{
	"state-backend":  {"rat://state/v1/get", "rat://state/v1/put", "rat://state/v1/list"},
	"secret-backend": {"rat://secret/v1/resolve"},
	"strategy":       {"rat://strategy/v1/apply"},
	"engine":         {"rat://engine/v1/execute"},
	"storage": {
		"rat://storage/v1/vend-credentials",
		"rat://storage/v1/vend-credentials-read",
		"rat://storage/v1/vend-credentials-write",
	},
}

// builtinVerbs are the top-level rat subcommands main() dispatches directly; a contributed command
// (ADR-041) may not shadow one — checked at `rat plugin check`.
var builtinVerbs = map[string]bool{
	"help": true, "serve": true, "up": true, "down": true, "status": true, "hub": true,
	"ls": true, "init": true, "add": true, "remove": true, "rm": true, "call": true,
	"apply": true, "version": true, "ui": true, "plugin": true, "search": true,
	"list": true, "marketplace": true, "market": true, "context": true, "ctx": true,
	"validate": true, "capabilities": true, "caps": true,
}

// validateCommands checks a plugin's contributed CLI commands (ADR-041): each names a real
// capability (in a linked axis), maps args to fields that exist on that capability's request, and
// does not shadow a built-in verb.
func validateCommands(m *manifest.Manifest) error {
	for _, c := range m.Contributes.Commands {
		if c.Name == "" || c.Capability == "" {
			return fmt.Errorf("check failed: %s contributes a command missing name or capability", m.Metadata.Name)
		}
		fields := strings.Fields(c.Name)
		if builtinVerbs[fields[0]] {
			return fmt.Errorf("check failed: %s command %q shadows the built-in verb %q", m.Metadata.Name, c.Name, fields[0])
		}
		if !linkedAxes()[capAxisOf(c.Capability)] {
			continue // axis not compiled into this rat — can't verify the field mapping; skip (noted)
		}
		_, in, _, err := resolveMethod(c.Capability)
		if err != nil {
			return fmt.Errorf("check failed: %s command %q → %q is not a real capability", m.Metadata.Name, c.Name, c.Capability)
		}
		for _, a := range c.Args {
			if !fieldPathExists(in, a.Field) {
				return fmt.Errorf("check failed: %s command %q arg %q maps to field %q absent on %s", m.Metadata.Name, c.Name, a.Name, a.Field, in.FullName())
			}
		}
	}
	return nil
}

// fieldPathExists reports whether a (possibly dotted) field path resolves on a message descriptor —
// e.g. "target.branch" descends into the `target` message field (ADR-041 nested arg-mapping).
func fieldPathExists(md protoreflect.MessageDescriptor, path string) bool {
	parts := strings.Split(path, ".")
	for i, p := range parts {
		fd := md.Fields().ByName(protoreflect.Name(p))
		if fd == nil {
			return false
		}
		if i == len(parts)-1 {
			return true
		}
		if fd.Kind() != protoreflect.MessageKind {
			return false
		}
		md = fd.Message()
	}
	return true
}

// validateAuthored is the authoring-gate validation `rat plugin check`/`pack` apply on top of the
// structural manifest.Load — it enforces the plugin.v1.json constraints the CLI used to skip
// (Gaps 3/8b), reconciled with the driver shape blessed by ADR-039:
//   - a plugin must DO SOMETHING: declare ≥1 `provides` (a provider) OR ≥1 `requires` (a driver).
//     This is the envelope's relaxed `provides` (minItems 0) + the "not empty" floor.
//   - `resources.requests` is MANDATORY (C4) — the scaffold now emits a default, so authored
//     plugins always carry it.
func validateAuthored(m *manifest.Manifest) error {
	if len(m.Provides) == 0 && len(m.Requires) == 0 {
		return fmt.Errorf("check failed: %s declares neither `provides` nor `requires` — a plugin must do something (a provider provides capabilities; a driver requires them; see ADR-039)", m.Metadata.Name)
	}
	if !m.HasResources() {
		return fmt.Errorf("check failed: %s is missing `resources.requests` (C4 — required by plugin.v1.json; declare cpu/memory asks)", m.Metadata.Name)
	}
	return nil
}

// manifestName loads a manifest file and returns its metadata.name (Gap 7: `rat add --manifest`
// derives the plugin name the same way `--image` derives it from the stamped manifest).
func manifestName(path string) (string, error) {
	m, err := manifest.Load(path)
	if err != nil {
		return "", err
	}
	return m.Metadata.Name, nil
}

// runPlugin dispatches the `rat plugin` subcommands.
func runPlugin(argv []string, out io.Writer) error {
	if len(argv) == 0 {
		printPluginHelp(out)
		return nil
	}
	sub, rest := argv[0], argv[1:]
	switch sub {
	case "-h", "--help", "help":
		printPluginHelp(out)
		return nil
	case "init":
		return runPluginInit(rest, out)
	case "check":
		return runPluginCheck(rest, out)
	case "dev":
		// the watch loop (DX-7): re-run check (+ test) on every change.
		return runPluginDev(rest, out)
	case "test":
		return runPluginTest(rest, out)
	case "pack":
		return runPluginPack(rest, out)
	case "publish":
		return runPluginPublish(rest, out)
	default:
		return fmt.Errorf("unknown `rat plugin %s` — run `rat plugin help`", sub)
	}
}

// printPluginHelp is what `rat plugin` (bare) / `rat plugin -h` shows.
func printPluginHelp(out io.Writer) {
	fmt.Fprint(out, `rat plugin — author, verify, and publish a plugin (ADR-026).

  init      scaffold a ready-to-build plugin folder
            rat plugin init <name> --kind <axis> --lang go|python|typescript|rust [--dir <path>]
  check     validate the manifest (static gate: schema + per-kind + dep coherence)
            rat plugin check [<dir>]
  dev       WATCH the dir; re-run check + test on every change (the inner loop)
            rat plugin dev [<dir>] [--interval 1s] [--check-only]
  test      launch the plugin + run its conformance vectors
            rat plugin test [<dir>] [--image <ref>]
  pack      build the image, STAMP the manifest in, verify it serves what it declares
            rat plugin pack [<dir>] [--image <ref>] [--tag <ref>]
  publish   push the verified image to a registry
            rat plugin publish [<dir>] [--registry <host/org>]

axes for --kind: engine runtime format strategy catalog storage deployment-runtime
  state-backend secret-backend scheduler-backend identity tenancy billing
  observability audit-log ui notifications marketplace

Each subcommand: `+"`rat plugin <sub> -h`"+` for its flags.
`)
}

// runPluginInit scaffolds a ready-to-build plugin folder (ADR-026): manifest + server stub +
// Dockerfile + README + portable CI/CD. The generated folder passes `rat plugin check`.
func runPluginInit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat plugin init", flag.ContinueOnError)
	// The plugin NAME is a leading positional, not a flag — spell that out in -h (Gap 1),
	// since flag's default usage only lists flags and would hide the required <name>.
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: rat plugin init <name> --kind <axis> [--lang python|go|typescript|rust] [--dir <path>]")
		fmt.Fprintln(fs.Output(), "  <name>   plugin name (a leading positional argument, required)")
		fs.PrintDefaults()
	}
	kind := fs.String("kind", "", "plugin kind (one of the 18 axes, e.g. state-backend, strategy)")
	lang := fs.String("lang", "python", "plugin language: python | go | typescript | rust")
	dir := fs.String("dir", "", "target directory (default: ./<name>)")
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" || *kind == "" {
		return fmt.Errorf("usage: rat plugin init <name> --kind <kind> [--lang python|go|typescript|rust] [--dir <path>]")
	}
	if !knownKinds[*kind] {
		return fmt.Errorf("unknown kind %q (must be one of the 18 axes — see plugin-architecture.md)", *kind)
	}
	canonLang, ok := canonicalLang(*lang)
	if !ok {
		return fmt.Errorf("--lang %q not supported (use: python | go | typescript | rust)", *lang)
	}
	target := *dir
	if target == "" {
		target = name
	}
	if _, err := os.Stat(filepath.Join(target, "manifest.yaml")); err == nil {
		return fmt.Errorf("%s/manifest.yaml already exists", target)
	}

	files := scaffold(name, *kind, canonLang)
	for rel, content := range files {
		p := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	fmt.Fprintf(out, "scaffolded %s/ (kind %s, %s, %d files) — next: cd %s && rat plugin check\n", target, *kind, canonLang, len(files), target)
	return nil
}

// canonicalLang normalizes a --lang value (with aliases) to one of the supported languages.
func canonicalLang(s string) (string, bool) {
	switch strings.ToLower(s) {
	case "py", "python":
		return "python", true
	case "go", "golang":
		return "go", true
	case "ts", "typescript":
		return "typescript", true
	case "rs", "rust":
		return "rust", true
	}
	return "", false
}

// runPluginCheck is the fast STATIC gate (ADR-026): load + validate the manifest (schema-subset
// via manifest.Load) + coherence (kind is a known axis; capabilities are well-formed URIs).
func runPluginCheck(args []string, out io.Writer) error {
	dir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir = args[0]
	}
	path := filepath.Join(dir, "manifest.yaml")
	if _, err := os.Stat(path); err != nil {
		// fall back to the *.plugin.yaml convention
		if ms, _ := filepath.Glob(filepath.Join(dir, "*.plugin.yaml")); len(ms) > 0 {
			path = ms[0]
		} else {
			return fmt.Errorf("no manifest.yaml (or *.plugin.yaml) in %s", dir)
		}
	}
	m, err := manifest.Load(path) // structural validation: kind, name, valid capability URIs
	if err != nil {
		return err
	}
	if !knownKinds[m.Kind] {
		return fmt.Errorf("check failed: unknown kind %q (not one of the 18 axes)", m.Kind)
	}
	if m.Metadata.Version == "" {
		return fmt.Errorf("check failed: missing metadata.version")
	}

	// DEPENDENCY COHERENCE (ADR-026 §3): a capability must NAME SOMETHING REAL, and a
	// plugin's `provides` must be its own axis. `requires` are legitimately cross-axis
	// (capability composition), so only their reality is checked, not their axis.
	linked := linkedAxes()
	unverified := 0
	checkReal := func(cap, role string) error {
		axis := capAxisOf(cap)
		if !linked[axis] {
			unverified++ // can't verify: this axis isn't compiled into this rat (not a typo, just unknown here)
			return nil
		}
		if _, _, _, err := resolveMethod(cap); err != nil {
			return fmt.Errorf("check failed: %s — %s %q is not a real capability of axis %q (typo?)", m.Metadata.Name, role, cap, axis)
		}
		return nil
	}
	for _, c := range m.ProvidesCaps() {
		if err := checkReal(c, "provides"); err != nil {
			return err
		}
	}
	for _, c := range m.RequiresCaps() {
		if err := checkReal(c, "requires"); err != nil {
			return err
		}
	}
	if want := kindAxis(m.Kind); want != "" {
		for _, c := range m.ProvidesCaps() {
			if a := capAxisOf(c); a != want {
				return fmt.Errorf("check failed: a %q plugin provides %q (axis %q) — expected %q-axis capabilities", m.Kind, c, a, want)
			}
		}
	}

	if err := validateAuthored(m); err != nil {
		return err
	}
	if err := validateCommands(m); err != nil {
		return err
	}

	note := ""
	if unverified > 0 {
		note = fmt.Sprintf(" (%d capabilit%s unverified — their axis isn't compiled into this rat)", unverified, plural(unverified))
	}
	fmt.Fprintf(out, "✓ %s (%s) — manifest + deps valid: %d provides, %d requires%s\n",
		m.Metadata.Name, m.Kind, len(m.Provides), len(m.Requires), note)
	return nil
}

// capAxisOf extracts the axis segment from a capability URI: "rat://state/v1/get" → "state".
func capAxisOf(cap string) string {
	s := strings.TrimPrefix(cap, "rat://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return ""
}

// kindAxis returns the axis a plugin of this kind serves (derived from the scaffold map, so
// it stays consistent), or "" when the kind has no well-known axis (skip coherence then).
func kindAxis(kind string) string {
	if caps := kindProvides[kind]; len(caps) > 0 {
		return capAxisOf(caps[0])
	}
	return ""
}

// linkedAxes is the set of axis segments compiled into THIS rat (from the capability
// annotations in the linked descriptors) — so `check` only HARD-FAILS a made-up capability
// in an axis it can actually see, and merely notes capabilities of axes it can't.
func linkedAxes() map[string]bool {
	axes := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			ms := svcs.Get(i).Methods()
			for j := 0; j < ms.Len(); j++ {
				if c, _ := proto.GetExtension(ms.Get(j).Options(), commonv1.E_Capability).(string); c != "" {
					if a := capAxisOf(c); a != "" {
						axes[a] = true
					}
				}
			}
		}
		return true
	})
	return axes
}

// runPluginTest is the strong gate (ADR-026): build the image (or use --image), LAUNCH it
// under the real I9 profile via the deployment-runtime, wait healthy, and verify the plugin
// actually SERVES each capability it declares in `provides` (a smoke invoke that must not be
// Unimplemented). Full golden-vector conformance is the next refinement (ADR-026 Q03).
func runPluginTest(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat plugin test", flag.ContinueOnError)
	image := fs.String("image", "", "test an already-built image (else build the dir's Dockerfile)")
	manifestPath := fs.String("manifest", "", "manifest path (default <dir>/manifest.yaml)")
	dir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	mp := *manifestPath
	if mp == "" {
		mp = filepath.Join(dir, "manifest.yaml")
	}
	m, err := manifest.Load(mp)
	if err != nil {
		return err
	}

	img := *image
	if img == "" {
		img = "localhost/rat-plugin-test/" + m.Metadata.Name + ":test"
		fmt.Fprintf(out, "building %s …\n", img)
		if b, err := exec.Command("podman", "build", "-t", img, dir).CombinedOutput(); err != nil {
			return fmt.Errorf("podman build: %v\n%s", err, tailString(string(b), 1500))
		}
	}

	if err := launchAndProbe(out, img, m); err != nil {
		return err
	}
	fmt.Fprintln(out, "  (golden-vector conformance is the next refinement — ADR-026 Q03)")
	return nil
}

// launchAndProbe is the verified-plugin core (shared by test + pack): launch the image under
// the real I9 profile, wait healthy, and verify it serves every capability it declares (a
// smoke invoke that must not be Unimplemented). Returns an error on the first failure.
func launchAndProbe(out io.Writer, img string, m *manifest.Manifest) error {
	iso, _ := isolationProfile("i9")
	rt := deploymentruntime.NewPodman()
	ctx := context.Background()
	lr, err := rt.Launch(ctx, &deploymentruntimev1.LaunchRequest{
		PluginId: m.Metadata.Name,
		Spec:     &deploymentruntimev1.LaunchSpec{Image: img, Isolation: iso},
	})
	if err != nil {
		return fmt.Errorf("launch under I9: %w", err)
	}
	defer rt.Terminate(ctx, &deploymentruntimev1.TerminateRequest{InstanceId: lr.GetInstanceId()})

	deadline := time.Now().Add(30 * time.Second)
	healthy := false
	for time.Now().Before(deadline) {
		hc, _ := rt.Healthcheck(ctx, &deploymentruntimev1.HealthcheckRequest{InstanceId: lr.GetInstanceId()})
		if hc.GetStatus() == deploymentruntimev1.HealthStatus_HEALTH_STATUS_HEALTHY {
			healthy = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !healthy {
		return fmt.Errorf("✗ %s never became healthy under the I9 profile", m.Metadata.Name)
	}
	fmt.Fprintf(out, "✓ launches under I9 (non-root · cap-drop ALL · read-only rootfs) + healthy at %s\n", lr.GetEndpoint())

	conn, err := grpc.NewClient(lr.GetEndpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, cap := range m.ProvidesCaps() {
		path, inD, outD, err := resolveMethod(cap)
		if err != nil {
			fmt.Fprintf(out, "  ? %s — %v (skipped: axis not linked into rat)\n", cap, err)
			continue
		}
		ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = conn.Invoke(ictx, path, dynamicpb.NewMessage(inD), dynamicpb.NewMessage(outD))
		cancel()
		if status.Code(err) == codes.Unimplemented {
			return fmt.Errorf("✗ %s declares %s but does NOT serve it (Unimplemented)", m.Metadata.Name, cap)
		}
		fmt.Fprintf(out, "  ✓ serves %s\n", cap)
	}
	fmt.Fprintf(out, "✓ %s PASSED — launches under I9 + serves its %d declared capabilit%s\n",
		m.Metadata.Name, len(m.ProvidesCaps()), plural(len(m.ProvidesCaps())))
	return nil
}

// manifestLabel is the OCI label `pack` stamps the validated manifest into (base64 YAML), so
// `rat add <ref>` can read the manifest FROM the image — no separate --manifest (ADR-026 Q05).
const manifestLabel = "dev.rat.manifest.v1.b64"

// readStampedManifest recovers the manifest from a packed image's label (manifest-from-image),
// returning both the validated manifest and its raw YAML bytes (so callers can re-materialize it).
func readStampedManifest(image string) (*manifest.Manifest, []byte, error) {
	got, err := exec.Command("podman", "inspect", "--format", "{{ index .Config.Labels \""+manifestLabel+"\" }}", image).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect %s: %w", image, err)
	}
	b64 := strings.TrimSpace(string(got))
	if b64 == "" || b64 == "<no value>" {
		return nil, nil, fmt.Errorf("%s has no stamped manifest — run `rat plugin pack` first, or pass --manifest", image)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode manifest label: %w", err)
	}
	td, err := os.MkdirTemp("", "rat-mf-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(td)
	p := filepath.Join(td, "manifest.yaml")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		return nil, nil, err
	}
	m, err := manifest.Load(p)
	return m, raw, err
}

// runPluginPublish ships a VERIFIED plugin image to a registry (ADR-026, the team diff). It
// reads the manifest (from --manifest or the image's stamped label), RE-VERIFIES the image
// (launch under I9 + serves — never publish a broken plugin), then tags + pushes it to
// <registry>/<name>:<version>. The registry is ghcr.io/<owner> in prod, or a local registry:2
// (localhost:5000, the "local packaging service") — same mechanism, push handles TLS/auth.
func runPluginPublish(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat plugin publish", flag.ContinueOnError)
	image := fs.String("image", "", "the local VERIFIED image to publish (from `rat plugin pack`)")
	registry := fs.String("registry", "", "target registry, e.g. ghcr.io/<owner> or localhost:5000")
	manifestPath := fs.String("manifest", "", "manifest path (default: read from the image's stamped label)")
	latest := fs.Bool("latest", false, "also push :latest")
	insecure := fs.Bool("insecure", false, "skip TLS verification (auto-on for localhost registries)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *image == "" || *registry == "" {
		return fmt.Errorf("usage: rat plugin publish --image <verified image> --registry <ghcr.io/owner|localhost:5000> [--latest] [--manifest <path>]")
	}

	var m *manifest.Manifest
	var err error
	if *manifestPath != "" {
		m, err = manifest.Load(*manifestPath)
	} else {
		m, _, err = readStampedManifest(*image)
	}
	if err != nil {
		return err
	}

	// Never publish a broken plugin: re-run the gate on the image being shipped.
	if err := launchAndProbe(out, *image, m); err != nil {
		return err
	}

	reg := strings.TrimSuffix(*registry, "/")
	tls := !*insecure && !isLocalRegistry(reg)
	remote := fmt.Sprintf("%s/%s:%s", reg, m.Metadata.Name, m.Metadata.Version)
	if err := pushImage(out, *image, remote, tls); err != nil {
		return err
	}
	if *latest {
		if err := pushImage(out, *image, fmt.Sprintf("%s/%s:latest", reg, m.Metadata.Name), tls); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "🚀 published %s (verified) — `rat add %s` to use it\n", remote, remote)
	return nil
}

func isLocalRegistry(reg string) bool {
	return strings.HasPrefix(reg, "localhost") || strings.HasPrefix(reg, "127.0.0.1")
}

// ensureImagePresent pulls an image ref if it isn't already in the local store (so a stamped
// manifest can be read from it — `rat add <ghcr-ref>`).
func ensureImagePresent(out io.Writer, ref string) error {
	if exec.Command("podman", "image", "exists", ref).Run() == nil {
		return nil
	}
	fmt.Fprintf(out, "pulling %s …\n", ref)
	pullArgs := []string{"pull"}
	// Insecure localhost registry (Gap 10): mirror publish's push-side handling so the pull
	// half doesn't fail with "http response to HTTPS client" on a plain local registry:2.
	if isLocalRegistry(ref) {
		pullArgs = append(pullArgs, "--tls-verify=false")
	}
	pullArgs = append(pullArgs, ref)
	if b, err := exec.Command("podman", pullArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("pull %s: %v\n%s", ref, err, tailString(string(b), 500))
	}
	return nil
}

func pushImage(out io.Writer, local, remote string, tls bool) error {
	if b, err := exec.Command("podman", "tag", local, remote).CombinedOutput(); err != nil {
		return fmt.Errorf("tag %s %s: %v\n%s", local, remote, err, b)
	}
	pushArgs := []string{"push"}
	if !tls {
		pushArgs = append(pushArgs, "--tls-verify=false")
	}
	pushArgs = append(pushArgs, remote)
	fmt.Fprintf(out, "pushing %s …\n", remote)
	if b, err := exec.Command("podman", pushArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("push %s: %v\n%s", remote, err, tailString(string(b), 800))
	}
	return nil
}

// runPluginPack is the full gate + the artifact (ADR-026): check → build with the manifest
// stamped in → launch+probe (test) → tag the VERIFIED, manifest-bearing image. Pass --image
// to stamp+verify an already-built image instead of building from a Dockerfile.
func runPluginPack(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat plugin pack", flag.ContinueOnError)
	from := fs.String("image", "", "stamp+verify an existing image (else build the dir's Dockerfile)")
	manifestPath := fs.String("manifest", "", "manifest path (default <dir>/manifest.yaml)")
	tag := fs.String("tag", "", "the verified image tag (default rat/<name>:<version>)")
	dir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	mp := *manifestPath
	if mp == "" {
		mp = filepath.Join(dir, "manifest.yaml")
	}
	m, err := manifest.Load(mp)
	if err != nil {
		return err
	}
	if !knownKinds[m.Kind] {
		return fmt.Errorf("pack: unknown kind %q", m.Kind)
	}
	if err := validateAuthored(m); err != nil {
		return err
	}
	finalTag := *tag
	if finalTag == "" {
		finalTag = fmt.Sprintf("localhost/rat/%s:%s", m.Metadata.Name, m.Metadata.Version)
	}

	raw, err := os.ReadFile(mp)
	if err != nil {
		return err
	}
	b64 := base64.StdEncoding.EncodeToString(raw)

	// Build the verified image with the manifest stamped as a label.
	if *from != "" {
		// derive from an existing image: FROM <image> + the label, built in a temp context.
		td, err := os.MkdirTemp("", "rat-pack-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(td)
		df := fmt.Sprintf("FROM %s\nLABEL %s=%s\n", *from, manifestLabel, b64)
		if err := os.WriteFile(filepath.Join(td, "Dockerfile"), []byte(df), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "stamping %s → %s …\n", *from, finalTag)
		if b, err := exec.Command("podman", "build", "-t", finalTag, td).CombinedOutput(); err != nil {
			return fmt.Errorf("podman build: %v\n%s", err, tailString(string(b), 1500))
		}
	} else {
		fmt.Fprintf(out, "building %s (manifest stamped) …\n", finalTag)
		if b, err := exec.Command("podman", "build", "--label", manifestLabel+"="+b64, "-t", finalTag, dir).CombinedOutput(); err != nil {
			return fmt.Errorf("podman build: %v\n%s", err, tailString(string(b), 1500))
		}
	}

	// Verify the stamped image (the test gate) on the FINAL tag.
	if err := launchAndProbe(out, finalTag, m); err != nil {
		return err
	}

	// Confirm the manifest is readable back from the image (what `rat add <ref>` will do).
	got, err := exec.Command("podman", "inspect", "--format", "{{ index .Config.Labels \""+manifestLabel+"\" }}", finalTag).Output()
	if err != nil || strings.TrimSpace(string(got)) != b64 {
		return fmt.Errorf("pack: manifest label not readable back from %s", finalTag)
	}
	fmt.Fprintf(out, "📦 packed %s — verified + manifest stamped (rat add %s reads it; rat plugin publish ships it)\n", finalTag, finalTag)
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// resolveMethod maps a capability URI to its gRPC method path + input/output message
// descriptors, by scanning the linked axis descriptors for the (rat.common.v1.capability)
// annotation — so `test` can smoke-invoke the capability directly on the launched plugin.
func resolveMethod(capURI string) (path string, in, out protoreflect.MessageDescriptor, err error) {
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			ms := svc.Methods()
			for j := 0; j < ms.Len(); j++ {
				meth := ms.Get(j)
				if c, _ := proto.GetExtension(meth.Options(), commonv1.E_Capability).(string); c == capURI {
					path = "/" + string(svc.FullName()) + "/" + string(meth.Name())
					in, out = meth.Input(), meth.Output()
					return false
				}
			}
		}
		return true
	})
	if path == "" {
		return "", nil, nil, fmt.Errorf("capability %q not declared by any linked axis", capURI)
	}
	return path, in, out, nil
}

// fill substitutes the __NAME__/__KIND__ placeholders in a raw stub template (avoids %-escaping
// hell with the languages' own format verbs/braces).
func fill(tmpl, name, kind string) string {
	return strings.NewReplacer("__NAME__", name, "__KIND__", kind).Replace(tmpl)
}

// kindToAxis maps a manifest kind to its capability-URI axis segment, for the kinds whose axis
// name differs from the kind name. Everything else uses the kind name as the axis.
var kindToAxis = map[string]string{
	"state-backend":      "state",
	"secret-backend":     "secret",
	"scheduler-backend":  "scheduler",
	"audit-log":          "auditlog",
	"deployment-runtime": "deploymentruntime",
}

func axisForKind(kind string) string {
	if a, ok := kindToAxis[kind]; ok {
		return a
	}
	return kind
}

// axisCap is one (capability URI, RPC method) pair an axis service declares.
type axisCap struct {
	cap    string // rat://<axis>/v1/<verb>
	method string // the RPC method name, e.g. "Get"
}

// axisServiceFromDescriptors finds, in the linked proto descriptors, the service that hosts an
// axis's capabilities and the (capability, method) pairs it declares — so the scaffold can derive
// the RIGHT provides + a kind-aware servicer stub instead of a hardcoded map (Gaps 4/8a). Returns
// a nil service when the axis isn't compiled into this rat (the scaffold falls back to generic).
func axisServiceFromDescriptors(axis string) (protoreflect.ServiceDescriptor, []axisCap) {
	var svc protoreflect.ServiceDescriptor
	var caps []axisCap
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			s := svcs.Get(i)
			ms := s.Methods()
			for j := 0; j < ms.Len(); j++ {
				m := ms.Get(j)
				if c, _ := proto.GetExtension(m.Options(), commonv1.E_Capability).(string); c != "" && capAxisOf(c) == axis {
					svc = s
					caps = append(caps, axisCap{cap: c, method: string(m.Name())})
				}
			}
		}
		return true
	})
	return svc, caps
}

// scaffold returns the generated files (relative path → content) for a plugin in `lang`. The
// manifest + CI are language-independent; langFiles supplies the server stub, build manifest,
// Dockerfile, and .gitignore. Every stub binds RAT_PLUGIN_ADDR and serves a gRPC server (so the
// folder builds + launches HEALTHY); you implement the servicer to pass `rat plugin pack`'s
// serves-gate.
func scaffold(name, kind, lang string) map[string]string {
	axis := axisForKind(kind)
	svc, axisCaps := axisServiceFromDescriptors(axis)

	var provides strings.Builder
	if len(axisCaps) == 0 {
		provides.WriteString("provides: []   # this axis is a driver shape, or its proto isn't linked into rat\n")
	} else {
		provides.WriteString("provides:   # the axis's capabilities — delete any you don't implement (a driver keeps none)\n")
		for _, c := range axisCaps {
			fmt.Fprintf(&provides, "  - capability: %s\n", c.cap)
		}
	}

	// C4: resources is REQUIRED by plugin.v1.json — scaffold a sane default so `check` passes.
	resources := "resources:\n  requests:\n    cpu: \"50m\"\n    memory: \"64Mi\"\n  limits:\n    cpu: \"500m\"\n    memory: \"256Mi\"\n"

	m := fmt.Sprintf(`# %s — a rat %s plugin (%s). Scaffolded by `+"`rat plugin init`"+` (ADR-026).
api_version: rat/1
kind: %s
metadata:
  name: %s
  version: 0.1.0
compatible_core: ["rat/1"]
%s%s`, name, kind, lang, kind, name, provides.String(), resources)

	// README: point at the axis CONTRACT + the actual capabilities to implement — NOT at a
	// sibling plugins/ tree that may not exist (Gap 2).
	var capList string
	for _, c := range axisCaps {
		capList += "- `" + c.cap + "`\n"
	}
	if capList == "" {
		capList = "(this axis serves no capability — it's a driver: declare `requires` in the manifest)\n"
	}
	readme := fmt.Sprintf("# %s\n\nA rat **%s** plugin in **%s** (scaffolded by `rat plugin init`, ADR-026).\n\n"+
		"```sh\nrat plugin check     # validate the manifest (static gate)\nrat plugin pack      # build + verify (launches under I9, must SERVE its declared capability)\nrat plugin publish   # push to ghcr.io\n```\n\n"+
		"## Implement\n\nThis axis lives at `contracts/proto/rat/%s/v1/` (see its `CONTRACT.md`). Capabilities:\n\n%s\n"+
		"The scaffolded server stub already imports the right axis stubs and lists each method — fill them in, then `rat plugin pack`.\n",
		name, kind, lang, axis, capList)

	ci := `#!/bin/sh
# ci.sh — the portable plugin CI/CD steps (ADR-026). The logic lives in rat verbs, so this
# runs unchanged on GitHub Actions, GitLab CI, or anywhere that can install rat + run shell.
set -eu
curl -fsSL https://github.com/squat-collective/rat-v3/releases/latest/download/install.sh | sh
./rat plugin check
./rat plugin pack
# CD (on a tag): ./rat plugin publish
`

	workflow := `# Plugin CI/CD (ADR-026) — a thin wrapper over the portable rat verbs (see ci.sh).
name: plugin
on:
  push:
    branches: [main]
    tags: ['v*']
  pull_request:
jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: install rat
        run: curl -fsSL https://github.com/squat-collective/rat-v3/releases/latest/download/install.sh | sh
      - name: check + pack
        run: ./rat plugin check && ./rat plugin pack
      - name: publish (tag → ghcr.io)            # CD: only on a version tag
        if: startsWith(github.ref, 'refs/tags/v')
        run: ./rat plugin publish --registry ghcr.io/${{ github.repository_owner }}
`

	files := map[string]string{
		"manifest.yaml":                m,
		"README.md":                    readme,
		"ci.sh":                        ci,
		".github/workflows/plugin.yml": workflow,
	}
	for rel, content := range langFiles(name, kind, lang, svc, axisCaps) {
		files[rel] = content
	}
	return files
}

// langFiles returns the language-specific scaffold (server stub + build manifest + Dockerfile +
// .gitignore). Compiled languages (Go, Rust) link the rat SDK in and ship a tiny static-ish
// binary — no SDK base image; interpreted ones (Python, TS) get the SDK from a base / npm.
func langFiles(name, kind, lang string, svc protoreflect.ServiceDescriptor, caps []axisCap) map[string]string {
	switch lang {
	case "go":
		return goFiles(name, kind)
	case "typescript":
		return tsFiles(name, kind)
	case "rust":
		return rustFiles(name, kind)
	default:
		return pyFiles(name, kind, svc, caps)
	}
}

// pyImportPath turns a service's proto file path into the Python SDK import coordinates:
// "rat/state/v1/state.proto" → pkg "rat.state.v1", module base "state".
func pyImportPath(svc protoreflect.ServiceDescriptor) (pkg, base string) {
	p := svc.ParentFile().Path()
	pkg = strings.ReplaceAll(filepath.Dir(p), "/", ".")
	base = strings.TrimSuffix(filepath.Base(p), ".proto")
	return pkg, base
}

// pyServerStub generates the Python server stub. When the axis service is linked into rat it is
// KIND-AWARE: right imports, the real Servicer base class, one pre-stubbed method per capability,
// and the matching registration call (Gap 4). Otherwise it falls back to a generic driver stub.
func pyServerStub(name, kind string, svc protoreflect.ServiceDescriptor, caps []axisCap) string {
	if svc == nil || len(caps) == 0 {
		return fmt.Sprintf(`"""%s — a rat %s plugin (Python). A DRIVER: it serves no capability (or its axis
isn't linked into rat). Drive other capabilities via plugin.Gateway() and declare them in the
manifest's `+"`requires`"+`. The rat SDK is provided by the plugin-base-py image (ADR-029)."""

from rat import plugin


def register(server):
    pass  # a driver serves nothing; its work runs in a background loop (see docs/guides/04)


def serve():
    plugin.serve(register)


if __name__ == "__main__":
    serve()
`, name, kind)
	}

	pkg, base := pyImportPath(svc)
	svcName := string(svc.Name()) // e.g. "StateService"
	cls := "My" + svcName

	var methods strings.Builder
	for _, c := range caps {
		fmt.Fprintf(&methods, "    def %s(self, request, context):\n        # %s\n        raise NotImplementedError(%q)\n\n", c.method, c.cap, c.method)
	}

	return fmt.Sprintf(`"""%s — a rat %s plugin (Python). Scaffolded kind-aware by `+"`rat plugin init`"+`.
Contract: %s (see its CONTRACT.md). Fill in each method below, then `+"`rat plugin pack`"+`.
The rat SDK is provided by the plugin-base-py image (ADR-029)."""

from rat import plugin
from %s import %s_pb2, %s_pb2_grpc  # noqa: F401 — %s_pb2 holds the request/response types


class %s(%s_pb2_grpc.%sServicer):
%s
def register(server):
    %s_pb2_grpc.add_%sServicer_to_server(%s(), server)


def serve():
    plugin.serve(register)


if __name__ == "__main__":
    serve()
`, name, kind, svc.ParentFile().Path(), pkg, base, base, base, cls, base, svcName, strings.TrimRight(methods.String(), "\n")+"\n", base, svcName, cls)
}

func pyFiles(name, kind string, svc protoreflect.ServiceDescriptor, caps []axisCap) map[string]string {
	server := pyServerStub(name, kind, svc, caps)

	dockerfile := `# __NAME__ plugin image — FROM the rat python base (the rat SDK + grpc are baked in, ADR-026).
# Your plugin repo carries only its OWN code; the SDK arrives via the base image, not vendored.
# Build the base once: ` + "`make plugin-base-py`" + ` in the rat repo (a published ghcr image later).
FROM localhost/rat/plugin-base-py:dev
WORKDIR /plugin
COPY . /plugin/
# your plugin's OWN extra deps (duckdb, boto3, …) go in requirements.txt; the rat SDK is already present.
RUN pip install --no-cache-dir -r requirements.txt
CMD ["python", "main.py"]
`
	return map[string]string{
		"server.py":        server,
		"main.py":          "from server import serve\n\nif __name__ == \"__main__\":\n    serve()\n",
		"requirements.txt": "# your plugin's own Python deps (the rat SDK + grpc come from the base image)\n",
		"Dockerfile":       fill(dockerfile, name, kind),
		".gitignore":       "__pycache__/\n*.pyc\n",
	}
}

func goFiles(name, kind string) map[string]string {
	main := fill(`// __NAME__ — a rat __KIND__ plugin (Go). Implement your servicer + register it in Serve.
// The rat SDK (gen stubs + the ratplugin runtime) is at /sdk via the build base; ships a tiny
// static binary on scratch (the SDK is compiled in). See ADR-029 for ratplugin.
package main

import (
	"github.com/squat-collective/rat-v3/gen/ratplugin"
	"google.golang.org/grpc"
	// TODO: import your axis stubs, e.g. secretv1 "github.com/squat-collective/rat-v3/gen/rat/secret/v1"
)

func main() {
	ratplugin.Serve(func(s grpc.ServiceRegistrar) {
		// TODO: register your servicer, e.g.
		//   secretv1.RegisterSecretServiceServer(s, &keyring{})
	})
}
`, name, kind)

	dockerfile := `# __NAME__ plugin image (Go) — built FROM the rat Go SDK base (the gen SDK at /sdk, replace'd
# in go.mod); ships a static binary on scratch (the SDK is compiled in, none at runtime).
# Build the base once: ` + "`make plugin-base-go`" + ` in the rat repo (a published module later).
FROM localhost/rat/plugin-base-go:dev AS build
ENV CGO_ENABLED=0 GOFLAGS=-mod=mod GOTOOLCHAIN=local
WORKDIR /src
COPY . .
RUN go mod tidy && go build -trimpath -o /plugin .
FROM scratch
ENV RAT_PLUGIN_ADDR=0.0.0.0:50051
COPY --from=build /plugin /plugin
ENTRYPOINT ["/plugin"]
`
	gomod := `module __NAME__

go 1.25

require google.golang.org/grpc v1.81.1

// the rat Go SDK — provided at /sdk by the plugin-base-go build image. Import the axis stubs
// you need, e.g. secretv1 "github.com/squat-collective/rat-v3/gen/rat/secret/v1"; ` + "`rat plugin pack`" + ` resolves it.
replace github.com/squat-collective/rat-v3/gen => /sdk
`
	return map[string]string{
		"main.go":    main,
		"go.mod":     fill(gomod, name, kind),
		"Dockerfile": fill(dockerfile, name, kind),
		".gitignore": "/plugin\n",
	}
}

func tsFiles(name, kind string) map[string]string {
	main := fill(`// __NAME__ — a rat __KIND__ plugin (TypeScript). Implement the servicer for your kind's
// capabilities; see plugins/ for a reference of the same kind.
import * as grpc from "@grpc/grpc-js";

const addr = process.env.RAT_PLUGIN_ADDR ?? "0.0.0.0:50051";
const server = new grpc.Server();
// TODO: server.addService(<service>Definition, new MyServicer());
server.bindAsync(addr, grpc.ServerCredentials.createInsecure(), (err) => {
  if (err) throw err;
  console.log("__NAME__ listening on " + addr);
});
`, name, kind)

	pkg := fill(`{
  "name": "__NAME__",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": { "build": "tsc" },
  "dependencies": { "@grpc/grpc-js": "^1.12.0" },
  "devDependencies": { "typescript": "^5.6.0", "@types/node": "^22.0.0" }
}
`, name, kind)

	tsconfig := `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "outDir": "dist",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true
  },
  "include": ["*.ts"]
}
`
	dockerfile := `# __NAME__ plugin image (TypeScript) — Node + @grpc/grpc-js. rat launches it under the I9 profile.
FROM docker.io/library/node:22-slim
ENV RAT_PLUGIN_ADDR=0.0.0.0:50051
WORKDIR /plugin
COPY package.json tsconfig.json ./
RUN npm install
COPY . .
RUN npm run build
CMD ["node", "dist/main.js"]
`
	return map[string]string{
		"main.ts":       main,
		"package.json":  pkg,
		"tsconfig.json": tsconfig,
		"Dockerfile":    fill(dockerfile, name, kind),
		".gitignore":    "node_modules/\ndist/\n",
	}
}

func rustFiles(name, kind string) map[string]string {
	main := fill(`// __NAME__ — a rat __KIND__ plugin (Rust, tonic). Implement the servicer for your kind's
// capabilities; see plugins/ for a reference. The stub serves a health service so it launches
// HEALTHY; add your axis service below.
use std::env;

use tonic::transport::Server;
use tonic_health::server::health_reporter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr = env::var("RAT_PLUGIN_ADDR").unwrap_or_else(|_| "0.0.0.0:50051".into());
    let (_reporter, health_service) = health_reporter();
    println!("__NAME__ listening on {addr}");
    Server::builder()
        // TODO: .add_service(<Axis>Server::new(MyServicer::default()))
        .add_service(health_service)
        .serve(addr.parse()?)
        .await?;
    Ok(())
}
`, name, kind)

	cargo := fill(`[package]
name = "__NAME__"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "plugin"
path = "src/main.rs"

[dependencies]
tonic = "0.12"
tonic-health = "0.12"
tokio = { version = "1", features = ["macros", "rt-multi-thread"] }
`, name, kind)

	dockerfile := `# __NAME__ plugin image (Rust) — tonic on distroless/cc. rat launches it under the I9 profile.
FROM docker.io/library/rust:1-slim AS build
WORKDIR /src
COPY . .
RUN cargo build --release
FROM gcr.io/distroless/cc-debian12
ENV RAT_PLUGIN_ADDR=0.0.0.0:50051
COPY --from=build /src/target/release/plugin /plugin
ENTRYPOINT ["/plugin"]
`
	return map[string]string{
		"src/main.rs": main,
		"Cargo.toml":  cargo,
		"Dockerfile":  fill(dockerfile, name, kind),
		".gitignore":  "/target\n",
	}
}
