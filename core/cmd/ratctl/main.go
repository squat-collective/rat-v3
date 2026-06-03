// Command ratctl is a standalone client for a running `rat serve` orchestrator —
// the kubectl to rat serve's apiserver. It is NOT part of the `rat` binary: clients
// are separate programs that connect to the gateway over gRPC and issue commands
// (ADR-019 — the orchestrator is one thing, clients another).
//
//	ratctl call <capability> --as <caller> [--data '<protojson>'] [--addr host:port]
//
// It is fully generic: it resolves the capability to its method (+ request/response
// message types) from the linked axis descriptors, builds the request from protojson,
// invokes it through the gateway carrying the caller identity C5 authorizes against,
// and prints the response as protojson. Example, against the Phase-A demo plane:
//
//	ratctl call rat://state/v1/get --as rat-caller --data '{"key":"k1"}'
//	ratctl call rat://state/v1/put --as rat-caller --data '{"key":"k1"}'   # PERMISSION_DENIED
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	commonv1 "github.com/rat-dev/rat/gen/rat/common/v1"
	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Blank imports register each axis's file descriptors + message types in
	// protoregistry.Global* — that is how ratctl resolves any of their capabilities
	// (capability → method → request/response type) without a hand-kept table.
	_ "github.com/rat-dev/rat/gen/rat/catalog/v1"
	_ "github.com/rat-dev/rat/gen/rat/engine/v1"
	_ "github.com/rat-dev/rat/gen/rat/format/v1"
	_ "github.com/rat-dev/rat/gen/rat/storage/v1"
	_ "github.com/rat-dev/rat/gen/rat/strategy/v1"
	// state/v1 is imported NAMED (statev1) above — `apply` builds a state.PutRequest
	// directly (binary project tarball, not protojson), so it needs the concrete type.
)

const callMetaHeader = "rat-callmeta-bin"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ratctl:", err)
		os.Exit(1)
	}
}

// run dispatches the subcommand. `call` issues one generic capability command; `apply`
// ships a project directory to the orchestrator (stored in the state-backend, picked up
// by the pipeline runner — ADR-021's "your pipeline is code you submit").
func run(argv []string, out io.Writer) error {
	if len(argv) < 1 {
		return fmt.Errorf("usage: ratctl <call|apply> … (call <capability> --as … | apply --project <dir> --name <name>)")
	}
	switch argv[0] {
	case "call":
		return runCall(argv, out)
	case "apply":
		return runApply(argv[1:], out)
	default:
		return fmt.Errorf("unknown command %q (want: call | apply)", argv[0])
	}
}

// runCall issues one capability command against the gateway and writes the response (as
// protojson) to out. It returns the raw invocation error on failure so a caller can
// inspect the gRPC status code (e.g. PermissionDenied for a C5 deny).
func runCall(argv []string, out io.Writer) error {
	if len(argv) < 2 || argv[0] != "call" {
		return fmt.Errorf("usage: ratctl call <capability> --as <caller> [--data '<protojson>'] [--addr host:port]")
	}
	capURI := argv[1]

	fs := flag.NewFlagSet("ratctl call", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7777", "rat serve gateway address")
	caller := fs.String("as", "", "caller plugin identity (must `requires` the capability — C5)")
	tenant := fs.String("tenant", "", "optional tenant identity")
	data := fs.String("data", "{}", "request body as protojson")
	timeout := fs.Duration("timeout", 10*time.Second, "call timeout")
	if err := fs.Parse(argv[2:]); err != nil {
		return err
	}
	if *caller == "" {
		return fmt.Errorf("--as <caller> is required (the gateway authorizes the command against the caller's declared `requires`)")
	}

	// 1. capability → request/response message types (from the linked descriptors).
	inName, outName, err := resolveCapability(capURI)
	if err != nil {
		return err
	}

	// 2. build the request message from the protojson body.
	reqMsg, err := newMessage(inName)
	if err != nil {
		return err
	}
	if err := protojson.Unmarshal([]byte(*data), reqMsg); err != nil {
		return fmt.Errorf("--data is not valid protojson for %s: %w", inName, err)
	}
	payload, err := proto.Marshal(reqMsg)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// 3. dial the gateway + issue the command, carrying the call-context envelope the
	//    gateway reads (traceparent for C1, caller identity for C5).
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial %s: %w", *addr, err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, callMetaHeader, callMeta(*caller, *tenant))

	resp, err := corev1.NewCapabilityInvokeServiceClient(conn).Invoke(ctx, &corev1.InvokeRequest{Capability: capURI, Payload: payload})
	if err != nil {
		return err // raw status (e.g. PermissionDenied) — let the caller read the code
	}

	// 4. decode + print the response as protojson.
	respMsg, err := newMessage(outName)
	if err != nil {
		return err
	}
	if err := proto.Unmarshal(resp.GetResult(), respMsg); err != nil {
		return fmt.Errorf("unmarshal response into %s: %w", outName, err)
	}
	b, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(respMsg)
	if err != nil {
		return fmt.Errorf("render response: %w", err)
	}
	fmt.Fprintln(out, string(b))
	return nil
}

// resolveCapability scans every registered axis service for the method carrying the
// (rat.common.v1.capability) annotation == capURI, and returns its input/output
// message full names. Mirrors the gateway's route derivation, client-side.
func resolveCapability(capURI string) (in, out protoreflect.FullName, err error) {
	var found bool
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			ms := svcs.Get(i).Methods()
			for j := 0; j < ms.Len(); j++ {
				m := ms.Get(j)
				if c, _ := proto.GetExtension(m.Options(), commonv1.E_Capability).(string); c == capURI {
					in, out, found = m.Input().FullName(), m.Output().FullName(), true
					return false
				}
			}
		}
		return true
	})
	if !found {
		return "", "", fmt.Errorf("unknown capability %q (no linked axis method declares it)", capURI)
	}
	return in, out, nil
}

// newMessage instantiates a registered message type by full name.
func newMessage(name protoreflect.FullName) (proto.Message, error) {
	mt, err := protoregistry.GlobalTypes.FindMessageByName(name)
	if err != nil {
		return nil, fmt.Errorf("message type %s not registered: %w", name, err)
	}
	return mt.New().Interface(), nil
}

// callMeta is the serialized RequestContext envelope: a well-formed traceparent (C1)
// + a correlation id + the caller identity (and optional tenant) C5 authorizes on.
func callMeta(caller, tenant string) string {
	rc := &commonv1.RequestContext{
		Trace:    &commonv1.TraceContext{Traceparent: newTraceparent(), CorrelationId: "ratctl-" + randHex(4)},
		Identity: &commonv1.Identity{CallerPlugin: caller, Tenant: tenant},
	}
	b, _ := proto.Marshal(rc)
	return string(b)
}

// newTraceparent builds a W3C traceparent: 00-<32 hex trace-id>-<16 hex span-id>-01.
func newTraceparent() string {
	return fmt.Sprintf("00-%s-%s-01", randHex(16), randHex(8))
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// runApply ships a project DIRECTORY (e.g. a dbt project — your pipeline as code, ADR-021)
// to the running orchestrator. The project is tar.gz'd client-side and stored in the
// state-backend at projects/<name> via rat://state/v1/put — so it travels the SAME
// C5-authorized, audited gateway path as any command, and needs NO new axis or proto: the
// state-backend IS the project store. The pipeline runner reads projects/<name> on its
// next run and executes YOUR code (not a baked-in copy). Re-apply overwrites it (a new
// state revision); the next scheduled run picks it up.
func runApply(argv []string, out io.Writer) error {
	fs := flag.NewFlagSet("ratctl apply", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7777", "rat serve gateway address")
	caller := fs.String("as", "platform-runner", "caller identity (must `requires` rat://state/v1/put — C5)")
	tenant := fs.String("tenant", "", "optional tenant identity")
	project := fs.String("project", "", "path to the project directory to ship")
	name := fs.String("name", "", "project name (stored at projects/<name>)")
	timeout := fs.Duration("timeout", 30*time.Second, "call timeout")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *project == "" || *name == "" {
		return fmt.Errorf("usage: ratctl apply --project <dir> --name <name> [--as <caller>] [--addr host:port]")
	}

	tarball, n, err := tarProject(*project)
	if err != nil {
		return fmt.Errorf("package project %s: %w", *project, err)
	}
	key := "projects/" + *name
	payload, err := proto.Marshal(&statev1.PutRequest{Key: key, Value: tarball})
	if err != nil {
		return fmt.Errorf("marshal put: %w", err)
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial %s: %w", *addr, err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, callMetaHeader, callMeta(*caller, *tenant))

	resp, err := corev1.NewCapabilityInvokeServiceClient(conn).Invoke(ctx, &corev1.InvokeRequest{Capability: "rat://state/v1/put", Payload: payload})
	if err != nil {
		return err // raw status (e.g. PermissionDenied if the caller can't write state)
	}
	var pr statev1.PutResponse
	if err := proto.Unmarshal(resp.GetResult(), &pr); err != nil {
		return fmt.Errorf("unmarshal put response: %w", err)
	}
	fmt.Fprintf(out, "applied %q → %s (%d files, %d bytes, revision %d)\n", *project, key, n, len(tarball), pr.GetRevision())
	return nil
}

// tarProject packages a project directory into a deterministic-ish tar.gz (generated /
// VCS noise excluded), returning the bytes and the file count. The runner untars this on
// the other side. Symlinks and irregular files are skipped (only dirs + regular files).
func tarProject(dir string) ([]byte, int, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, 0, err
	}
	if !info.IsDir() {
		return nil, 0, fmt.Errorf("%s is not a directory", dir)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	count := 0
	err = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skipProjectPath(filepath.Base(rel)) {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !fi.IsDir() && !fi.Mode().IsRegular() {
			return nil // skip symlinks / devices / sockets
		}
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if err := tw.Close(); err != nil {
		return nil, 0, err
	}
	if err := gz.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), count, nil
}

// skipProjectPath drops generated / VCS / local-state noise so `apply` ships only source.
func skipProjectPath(base string) bool {
	switch base {
	case ".git", "target", "logs", "dbt_packages", "__pycache__", ".gitignore", ".DS_Store":
		return true
	}
	return strings.HasSuffix(base, ".duckdb") || strings.HasSuffix(base, ".duckdb.wal") || strings.HasSuffix(base, ".pyc")
}
