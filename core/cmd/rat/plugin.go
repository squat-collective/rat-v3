package main

// plugin.go — the `rat plugin` authoring toolkit (ADR-026): build-time verbs for creating,
// validating, packaging, and publishing plugins. This prototype lands the two provable,
// immediately-useful ones: `init` (scaffold a ready-to-build plugin folder, poetry-init style)
// and `check` (the fast STATIC gate — manifest schema + per-kind + coherence). `test`/`pack`/
// `publish` (launch+conformance, verified image, GHCR) are the next slices.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rat-dev/rat/core/manifest"
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
	case "test", "pack", "publish":
		return fmt.Errorf("`rat plugin %s` is not built yet (ADR-026 next slice — launch+conformance / verified image / GHCR)", sub)
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
