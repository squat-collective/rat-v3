package client

// command.go — the pluggable-CLI dispatcher (ADR-041). `rat <tokens…>` that don't match a built-in
// verb are resolved here: read the contributed-command index (cli/commands/*) from the connected
// gateway, longest-prefix-match the command name, map CLI positionals/flags onto the capability's
// request message fields, and invoke it through the gateway (C5 + audit). Remote by construction —
// it's a plain gateway client (--addr/--workspace/--token), so it works through a hub to a remote
// workspace exactly like `rat call`.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/core/v1"
	statev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// cliCommandArg / cliCommand mirror the JSON a plugin publishes at cli/commands/<plugin>/<name>
// (the manifest.Command shape).
type cliCommandArg struct {
	Name       string `json:"name"`
	Field      string `json:"field"`
	Positional bool   `json:"positional"`
	Required   bool   `json:"required"`
	Default    string `json:"default"`
}

type cliCommand struct {
	Name       string          `json:"name"`
	Capability string          `json:"capability"`
	Help       string          `json:"help"`
	Args       []cliCommandArg `json:"args"`
	source     string          // contributing plugin (from the state key)
}

// RunCommand dispatches a plugin-contributed command. argv is everything after the (unmatched)
// leading token — i.e. the full original args, since the command name itself may be multi-token.
func RunCommand(argv []string, out io.Writer) error {
	// Pull the dispatcher's own flags (addr/as/token/workspace) out first, leaving the command
	// tokens + the command's own --flags in `rest`. Defaults come from the current `rat context`.
	c0 := CurrentContext()
	addr, caller, token, workspace := c0.Addr, c0.As, c0.Token, c0.Workspace
	var rest []string
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--addr", "-addr":
			i++
			if i < len(argv) {
				addr = argv[i]
			}
		case "--as", "-as":
			i++
			if i < len(argv) {
				caller = argv[i]
			}
		case "--token", "-token":
			i++
			if i < len(argv) {
				token = argv[i]
			}
		case "--workspace", "-workspace":
			i++
			if i < len(argv) {
				workspace = argv[i]
			}
		default:
			rest = append(rest, argv[i])
		}
	}
	if caller == "" {
		return fmt.Errorf("--as <caller> is required (the gateway authorizes the command against the caller's declared `requires`)")
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()
	gw := corev1.NewCapabilityInvokeServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, callMetaHeader, callMeta(caller, ""))
	if workspace != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "rat-workspace", workspace)
	}
	if token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "rat-token", token)
	}

	cmds, err := readCommands(ctx, gw)
	if err != nil {
		return err
	}
	cmd, tail := matchCommand(cmds, rest)
	if cmd == nil {
		return unknownCommandErr(rest, cmds)
	}
	return dispatch(ctx, gw, cmd, tail, out)
}

// readCommands pulls the contributed-command index (cli/commands/*) from the state-backend.
func readCommands(ctx context.Context, gw corev1.CapabilityInvokeServiceClient) ([]cliCommand, error) {
	listPayload, _ := proto.Marshal(&statev1.ListRequest{Prefix: "cli/commands/"})
	lr, err := gw.Invoke(ctx, &corev1.InvokeRequest{Capability: "rat://state/v1/list", Payload: listPayload})
	if err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	var lresp statev1.ListResponse
	if err := proto.Unmarshal(lr.GetResult(), &lresp); err != nil {
		return nil, err
	}
	var cmds []cliCommand
	for _, key := range lresp.GetKeys() {
		getPayload, _ := proto.Marshal(&statev1.GetRequest{Key: key})
		gr, err := gw.Invoke(ctx, &corev1.InvokeRequest{Capability: "rat://state/v1/get", Payload: getPayload})
		if err != nil {
			continue
		}
		var gresp statev1.GetResponse
		if proto.Unmarshal(gr.GetResult(), &gresp) != nil || !gresp.GetFound() {
			continue
		}
		var c cliCommand
		if json.Unmarshal(gresp.GetValue(), &c) != nil {
			continue
		}
		if parts := strings.Split(key, "/"); len(parts) >= 3 { // cli/commands/<plugin>/<name>
			c.source = parts[2]
		}
		cmds = append(cmds, c)
	}
	return cmds, nil
}

// matchCommand longest-prefix-matches a command name (space-separated tokens) against the leading
// tokens of `rest`, returning the command + the remaining tokens (its args).
func matchCommand(cmds []cliCommand, rest []string) (*cliCommand, []string) {
	var best *cliCommand
	bestLen := 0
	for i := range cmds {
		name := strings.Fields(cmds[i].Name)
		if len(name) > len(rest) || len(name) <= bestLen {
			continue
		}
		ok := true
		for j, tok := range name {
			if rest[j] != tok {
				ok = false
				break
			}
		}
		if ok {
			best = &cmds[i]
			bestLen = len(name)
		}
	}
	if best == nil {
		return nil, nil
	}
	return best, rest[bestLen:]
}

// dispatch binds the command's args from `tail`, builds the request, invokes the capability, and
// renders the response.
func dispatch(ctx context.Context, gw corev1.CapabilityInvokeServiceClient, cmd *cliCommand, tail []string, out io.Writer) error {
	inName, outName, err := resolveCapability(cmd.Capability)
	if err != nil {
		return err
	}
	reqMsg, err := newMessage(inName)
	if err != nil {
		return err
	}
	if err := bindArgs(reqMsg, cmd, tail); err != nil {
		return err
	}
	payload, err := proto.Marshal(reqMsg)
	if err != nil {
		return err
	}
	resp, err := gw.Invoke(ctx, &corev1.InvokeRequest{Capability: cmd.Capability, Payload: payload})
	if err != nil {
		return err // raw gRPC status (e.g. PermissionDenied when the caller can't call it)
	}
	respMsg, err := newMessage(outName)
	if err != nil {
		return err
	}
	if err := proto.Unmarshal(resp.GetResult(), respMsg); err != nil {
		return err
	}
	b, _ := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(respMsg)
	fmt.Fprintln(out, string(b))
	return nil
}

// bindArgs maps the CLI tail (positionals + --flags) onto the request message fields per the
// command's arg spec.
func bindArgs(msg proto.Message, cmd *cliCommand, tail []string) error {
	values := map[string]string{}       // CLI arg name -> raw value
	var positionals []string            // bare tokens, in order
	for i := 0; i < len(tail); i++ {
		t := tail[i]
		if strings.HasPrefix(t, "--") {
			name := strings.TrimPrefix(t, "--")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				values[name[:eq]] = name[eq+1:]
				continue
			}
			i++
			if i < len(tail) {
				values[name] = tail[i]
			}
		} else {
			positionals = append(positionals, t)
		}
	}
	pi := 0
	fields := msg.ProtoReflect().Descriptor().Fields()
	for _, a := range cmd.Args {
		var raw string
		var present bool
		if a.Positional {
			if pi < len(positionals) {
				raw, present, pi = positionals[pi], true, pi+1
			}
		} else if v, ok := values[a.Name]; ok {
			raw, present = v, true
		}
		if !present {
			if a.Default != "" {
				raw, present = a.Default, true
			} else if a.Required {
				return fmt.Errorf("command %q: missing required arg %q", cmd.Name, a.Name)
			} else {
				continue
			}
		}
		if err := setFieldPath(msg.ProtoReflect(), a.Field, raw); err != nil {
			return fmt.Errorf("command %q arg %q: %w", cmd.Name, a.Name, err)
		}
	}
	_ = fields
	return nil
}

// setFieldPath sets a scalar onto a (possibly nested, dotted) field path, e.g. "target.branch"
// descends into the `target` message and sets its `branch` field.
func setFieldPath(m protoreflect.Message, path, raw string) error {
	parts := strings.Split(path, ".")
	for i, p := range parts {
		fd := m.Descriptor().Fields().ByName(protoreflect.Name(p))
		if fd == nil {
			return fmt.Errorf("unknown field %q", p)
		}
		if i == len(parts)-1 {
			v, err := coerce(fd, raw)
			if err != nil {
				return err
			}
			m.Set(fd, v)
			return nil
		}
		if fd.Kind() != protoreflect.MessageKind {
			return fmt.Errorf("field %q is not a message (cannot descend to %q)", p, path)
		}
		m = m.Mutable(fd).Message()
	}
	return nil
}

// coerce turns a CLI string into the protoreflect.Value for a scalar field (the v1 grammar covers
// scalars; nested/repeated fields fall back to `rat call --data`).
func coerce(fd protoreflect.FieldDescriptor, raw string) (protoreflect.Value, error) {
	switch fd.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(raw), nil
	case protoreflect.BoolKind:
		b, err := strconv.ParseBool(raw)
		return protoreflect.ValueOfBool(b), err
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := strconv.ParseInt(raw, 10, 32)
		return protoreflect.ValueOfInt32(int32(n)), err
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := strconv.ParseInt(raw, 10, 64)
		return protoreflect.ValueOfInt64(n), err
	case protoreflect.DoubleKind:
		f, err := strconv.ParseFloat(raw, 64)
		return protoreflect.ValueOfFloat64(f), err
	case protoreflect.BytesKind:
		return protoreflect.ValueOfBytes([]byte(raw)), nil
	default:
		return protoreflect.Value{}, fmt.Errorf("unsupported field kind %s (use `rat call --data`)", fd.Kind())
	}
}

func unknownCommandErr(rest []string, cmds []cliCommand) error {
	first := ""
	if len(rest) > 0 {
		first = rest[0]
	}
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("unknown command %q — and no plugin commands are available (is the platform up + reachable at --addr?)", first)
	}
	return fmt.Errorf("unknown command %q — available plugin commands: %s", first, strings.Join(names, ", "))
}

// ListCommands fetches the contributed-command table for `rat help` (best-effort; remote-aware).
func ListCommands(addr, caller string, out io.Writer) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, callMetaHeader, callMeta(caller, ""))
	cmds, err := readCommands(ctx, corev1.NewCapabilityInvokeServiceClient(conn))
	if err != nil || len(cmds) == 0 {
		return
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	fmt.Fprintln(out, "\nPLUGIN COMMANDS (contributed; from the connected gateway)")
	for _, c := range cmds {
		fmt.Fprintf(out, "  %-22s %s\n", c.Name, c.Help)
	}
}
