package main

// control_client.go — the CLI side of live control (ADR-027). After `rat add`/`rat remove`
// edits rat.toml (the source of truth), if a daemon is RUNNING for the project it dials the
// control listener and materializes the diff live (RegisterPlugin/DeregisterPlugin) — no
// restart. With no daemon up (or --no-live) the edit stays declarative and `rat up` applies it.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// projectDaemonAddr returns the running daemon's control address for this project, or
// ("", false) if none is running. The address is the project's resolved control addr (a
// per-project unix socket by default, ADR-023).
func projectDaemonAddr(tomlPath, dir string) (string, bool) {
	if _, alive := runningPid(filepath.Join(dir, ".rat")); !alive {
		return "", false
	}
	pl, err := LoadProject(tomlPath)
	if err != nil {
		return "", false
	}
	return pl.Addr, true
}

// dialControl opens a ControlService client to the daemon at addr (a "unix:<path>" socket
// or a TCP host:port). Caller closes the returned conn.
func dialControl(addr string) (corev1.ControlServiceClient, *grpc.ClientConn, error) {
	target := addr
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		path = strings.TrimPrefix(path, "//")
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		target = "unix://" + path
	}
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return corev1.NewControlServiceClient(conn), conn, nil
}

// materializeAdd registers a just-added plugin with the running daemon. manifestRel is the
// plugin's manifest path (as stored in rat.toml); image/isolation/env mirror the [[plugin]]
// block. An empty image registers a driver (no launch).
func materializeAdd(out io.Writer, addr, dir, name, manifestRel, image, isolation string, envs multiFlag) error {
	mfBytes, err := os.ReadFile(resolve(dir, manifestRel))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var launch *corev1.LaunchSpec
	if image != "" {
		env := map[string]string{}
		for _, kv := range envs {
			if k, v, ok := strings.Cut(kv, "="); ok {
				env[k] = v
			}
		}
		launch = &corev1.LaunchSpec{Image: image, Isolation: isolation, Env: env}
	}
	client, conn, err := dialControl(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := client.RegisterPlugin(ctx, &corev1.RegisterPluginRequest{
		Name: name, ManifestYaml: mfBytes, Launch: launch,
	})
	if err != nil {
		return err
	}
	if resp.GetEndpoint() != "" {
		fmt.Fprintf(out, "  ● live: registered %q with the running daemon — %s at %s\n", name, resp.GetState(), resp.GetEndpoint())
	} else {
		fmt.Fprintf(out, "  ● live: registered %q with the running daemon (%s)\n", name, resp.GetState())
	}
	return nil
}

// materializeRemove deregisters a just-removed plugin from the running daemon.
func materializeRemove(out io.Writer, addr, name string) error {
	client, conn, err := dialControl(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := client.DeregisterPlugin(ctx, &corev1.DeregisterPluginRequest{Name: name})
	if err != nil {
		return err
	}
	if resp.GetWasPresent() {
		fmt.Fprintf(out, "  ● live: deregistered %q from the running daemon\n", name)
	} else {
		fmt.Fprintf(out, "  ● live: %q was not in the running daemon (no-op)\n", name)
	}
	return nil
}
