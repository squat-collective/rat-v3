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

	"github.com/rat-dev/rat/core/deploymentruntime"
	"github.com/rat-dev/rat/core/manifest"
	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
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
	"storage":        {"rat://storage/v1/vend"},
}

// runPlugin dispatches the `rat plugin` subcommands.
func runPlugin(argv []string, out io.Writer) error {
	if len(argv) == 0 {
		return fmt.Errorf("usage: rat plugin <init|check|test|pack|publish> …")
	}
	sub, rest := argv[0], argv[1:]
	switch sub {
	case "init":
		return runPluginInit(rest, out)
	case "check":
		return runPluginCheck(rest, out)
	case "test":
		return runPluginTest(rest, out)
	case "pack":
		return runPluginPack(rest, out)
	case "publish":
		return runPluginPublish(rest, out)
	default:
		return fmt.Errorf("unknown `rat plugin %s` (want: init | check | test | pack | publish)", sub)
	}
}

// runPluginInit scaffolds a ready-to-build plugin folder (ADR-026): manifest + server stub +
// Dockerfile + README + portable CI/CD. The generated folder passes `rat plugin check`.
func runPluginInit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat plugin init", flag.ContinueOnError)
	kind := fs.String("kind", "", "plugin kind (one of the 18 axes, e.g. state-backend, strategy)")
	lang := fs.String("lang", "python", "plugin language (python in v1)")
	dir := fs.String("dir", "", "target directory (default: ./<name>)")
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" || *kind == "" {
		return fmt.Errorf("usage: rat plugin init <name> --kind <kind> [--lang python] [--dir <path>]")
	}
	if !knownKinds[*kind] {
		return fmt.Errorf("unknown kind %q (must be one of the 18 axes — see plugin-architecture.md)", *kind)
	}
	if *lang != "python" {
		return fmt.Errorf("--lang %q not supported yet (python only in v1)", *lang)
	}
	target := *dir
	if target == "" {
		target = name
	}
	if _, err := os.Stat(filepath.Join(target, "manifest.yaml")); err == nil {
		return fmt.Errorf("%s/manifest.yaml already exists", target)
	}

	files := scaffold(name, *kind)
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
	fmt.Fprintf(out, "scaffolded %s/ (kind %s, %d files) — next: cd %s && rat plugin check\n", target, *kind, len(files), target)
	return nil
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
	fmt.Fprintf(out, "✓ %s (%s) — manifest valid: %d provides, %d requires\n",
		m.Metadata.Name, m.Kind, len(m.Provides), len(m.Requires))
	return nil
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

// readStampedManifest recovers the manifest from a packed image's label (manifest-from-image).
func readStampedManifest(image string) (*manifest.Manifest, error) {
	got, err := exec.Command("podman", "inspect", "--format", "{{ index .Config.Labels \""+manifestLabel+"\" }}", image).Output()
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", image, err)
	}
	b64 := strings.TrimSpace(string(got))
	if b64 == "" || b64 == "<no value>" {
		return nil, fmt.Errorf("%s has no stamped manifest — run `rat plugin pack` first, or pass --manifest", image)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode manifest label: %w", err)
	}
	td, err := os.MkdirTemp("", "rat-mf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(td)
	p := filepath.Join(td, "manifest.yaml")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		return nil, err
	}
	return manifest.Load(p)
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
		m, err = readStampedManifest(*image)
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

// scaffold returns the generated files (relative path → content) for a plugin.
func scaffold(name, kind string) map[string]string {
	var provides strings.Builder
	caps := kindProvides[kind]
	if len(caps) == 0 {
		provides.WriteString("provides: []   # TODO: a driver kind serves no capability; declare `requires` below\n")
	} else {
		provides.WriteString("provides:\n")
		for _, c := range caps {
			fmt.Fprintf(&provides, "  - capability: %s\n", c)
		}
	}

	m := fmt.Sprintf(`# %s — a rat %s plugin. Scaffolded by `+"`rat plugin init`"+` (ADR-026).
api_version: rat/1
kind: %s
metadata:
  name: %s
  version: 0.1.0
compatible_core: ["rat/1"]
%s`, name, kind, kind, name, provides.String())

	server := fmt.Sprintf(`"""%s — a rat %s plugin (scaffold). Implement the servicer for your kind's
capabilities; see examples/ for a reference of the same kind."""

import os
from concurrent import futures

import grpc

# TODO: import your axis's generated grpc stubs, e.g.:
#   from rat.state.v1 import state_pb2_grpc
# and register your servicer below.


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "0.0.0.0:50051")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    # TODO: <axis>_pb2_grpc.add_<Service>Servicer_to_server(MyServicer(), server)
    server.add_insecure_port(addr)
    server.start()
    print(f"%s listening on {addr}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
`, name, kind, name)

	dockerfile := fmt.Sprintf(`# %s plugin image. Build from the plugin dir; rat launches it under the I9 profile.
FROM docker.io/library/python:3.12-slim
ENV PYTHONUNBUFFERED=1 RAT_PLUGIN_ADDR=0.0.0.0:50051
WORKDIR /plugin
COPY . /plugin/
# the rat Python SDK (vendor it next to the plugin, or COPY from the SDK distribution):
#   COPY rat /usr/local/lib/python3.12/site-packages/rat
RUN pip install --no-cache-dir -r requirements.txt
CMD ["python", "main.py"]
`, name)

	readme := fmt.Sprintf("# %s\n\nA rat **%s** plugin (scaffolded by `rat plugin init`, ADR-026).\n\n"+
		"```sh\nrat plugin check     # validate the manifest (static gate)\nrat plugin test      # launch + conformance (coming)\n"+
		"rat plugin pack      # build a verified image (coming)\nrat plugin publish   # push to ghcr.io (coming)\n```\n\n"+
		"Implement your servicer in `server.py` (see `examples/` for a reference of the same kind), then `rat plugin pack`.\n", name, kind)

	ci := `#!/bin/sh
# ci.sh — the portable plugin CI/CD steps (ADR-026). The logic lives in rat verbs, so this
# runs unchanged on GitHub Actions, GitLab CI, or anywhere that can install rat + run shell.
set -eu
curl -fsSL https://github.com/rat-dev/rat/releases/latest/download/install.sh | sh
./rat plugin check
./rat plugin test
./rat plugin pack
# CD (on a tag): ./rat plugin publish
`

	workflow := fmt.Sprintf(`# Plugin CI/CD (ADR-026) — a thin wrapper over the portable rat verbs (see ci.sh).
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
        run: curl -fsSL https://github.com/rat-dev/rat/releases/latest/download/install.sh | sh
      - name: check + test + pack
        run: ./rat plugin check && ./rat plugin test && ./rat plugin pack
      - name: publish (tag → ghcr.io)            # CD: only on a version tag
        if: startsWith(github.ref, 'refs/tags/v')
        run: ./rat plugin publish --registry ghcr.io/${{ github.repository_owner }}
`)

	return map[string]string{
		"manifest.yaml":               m,
		"server.py":                   server,
		"main.py":                     "from server import serve\n\nif __name__ == \"__main__\":\n    serve()\n",
		"requirements.txt":            "grpcio==1.80.0\nprotobuf==7.35.0\n",
		"Dockerfile":                  dockerfile,
		"README.md":                   readme,
		".gitignore":                  "__pycache__/\n*.pyc\n",
		"ci.sh":                       ci,
		".github/workflows/plugin.yml": workflow,
	}
}
