package client

// ui.go — the CLI SURFACE consumer (ADR-025). `rat ui` is a surface consumer: a
// direct-to-gateway client (nothing of it runs in the daemon) that reads the contributions
// targeted at the `cli` surface — published by stack plugins to ui/components/<plugin>/<id>
// in the state-backend — and renders/invokes them in the terminal. It pulls ONLY its
// surface, so the same plugin's vscode/webapp interfaces are invisible here (conditional by
// consumption). Contributed commands fire their capability through the gateway (C5 + audit).
//
//	rat ui [--surface cli] [--as <caller>] [--addr host:port]   # list this surface's contributions
//	rat ui run <id> [--as <caller>] [--addr host:port]          # invoke a command's capability

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "github.com/rat-dev/rat/gen/rat/core/v1"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// uiComponent is one published contribution (the runtime spec at ui/components/<plugin>/<id>).
type uiComponent struct {
	Slot       string          `json:"slot"`
	Surface    string          `json:"surface"`
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Capability string          `json:"capability"`
	Args       json.RawMessage `json:"args"`
	source     string          // the contributing plugin (from the state key)
}

// matchesSurface: a component is shown on a surface if it targets that surface, or is
// surface-agnostic ("", "*", "generic"). Absence of a surface consumer makes a contribution
// inert; this is the consumer side of that filter.
func matchesSurface(c uiComponent, surface string) bool {
	switch c.Surface {
	case surface, "", "*", "generic":
		return true
	}
	return false
}

// RunUI is the `rat ui` surface consumer.
func RunUI(argv []string, out io.Writer) error {
	sub, rest, runID := "list", argv, ""
	if len(argv) > 0 && argv[0] == "run" {
		if len(argv) < 2 {
			return fmt.Errorf("usage: rat ui run <id>")
		}
		sub, runID, rest = "run", argv[1], argv[2:]
	} else if len(argv) > 0 && argv[0] == "list" {
		rest = argv[1:]
	}
	ctx0 := CurrentContext()
	uiCaller := ctx0.As
	if uiCaller == "" {
		uiCaller = "platform-runner"
	}
	fs := flag.NewFlagSet("rat ui", flag.ContinueOnError)
	addr := fs.String("addr", ctx0.Addr, "gateway address")
	caller := fs.String("as", uiCaller, "consumer identity (must `requires` state read + the command capabilities)")
	surface := fs.String("surface", "cli", "the surface to render")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial %s: %w", *addr, err)
	}
	defer conn.Close()
	gw := corev1.NewCapabilityInvokeServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, callMetaHeader, callMeta(*caller, ""))

	comps, err := readContributions(ctx, gw)
	if err != nil {
		return err
	}
	var shown []uiComponent
	for _, c := range comps {
		if matchesSurface(c, *surface) {
			shown = append(shown, c)
		}
	}

	if sub == "run" {
		return runComponent(ctx, gw, shown, runID, out)
	}
	return renderList(shown, *surface, out)
}

// readContributions pulls every ui/components/* spec from the state-backend through the gateway.
func readContributions(ctx context.Context, gw corev1.CapabilityInvokeServiceClient) ([]uiComponent, error) {
	listPayload, _ := proto.Marshal(&statev1.ListRequest{Prefix: "ui/components/"})
	lr, err := gw.Invoke(ctx, &corev1.InvokeRequest{Capability: "rat://state/v1/list", Payload: listPayload})
	if err != nil {
		return nil, fmt.Errorf("list contributions: %w", err)
	}
	var lresp statev1.ListResponse
	if err := proto.Unmarshal(lr.GetResult(), &lresp); err != nil {
		return nil, err
	}
	var out []uiComponent
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
		var c uiComponent
		if json.Unmarshal(gresp.GetValue(), &c) != nil {
			continue
		}
		if parts := strings.Split(key, "/"); len(parts) >= 3 { // ui/components/<plugin>/<id>
			c.source = parts[2]
		}
		out = append(out, c)
	}
	return out, nil
}

// renderList prints the surface's contributions grouped by slot.
func renderList(comps []uiComponent, surface string, out io.Writer) error {
	if len(comps) == 0 {
		fmt.Fprintf(out, "no contributions for surface %q (is the platform up?)\n", surface)
		return nil
	}
	bySlot := map[string][]uiComponent{}
	for _, c := range comps {
		bySlot[c.Slot] = append(bySlot[c.Slot], c)
	}
	slots := make([]string, 0, len(bySlot))
	for s := range bySlot {
		slots = append(slots, s)
	}
	sort.Strings(slots)
	fmt.Fprintf(out, "surface %q:\n", surface)
	for _, slot := range slots {
		fmt.Fprintf(out, "  [%s]\n", slot)
		for _, c := range bySlot[slot] {
			extra := ""
			if slot == "command" {
				extra = "   (rat ui run " + c.ID + ")"
			}
			fmt.Fprintf(out, "     %-16s %-22s from %s%s\n", c.ID, c.Title, c.source, extra)
		}
	}
	return nil
}

// runComponent invokes a command contribution's capability through the gateway.
func runComponent(ctx context.Context, gw corev1.CapabilityInvokeServiceClient, comps []uiComponent, id string, out io.Writer) error {
	var cmd *uiComponent
	for i := range comps {
		if comps[i].ID == id && comps[i].Slot == "command" {
			cmd = &comps[i]
			break
		}
	}
	if cmd == nil {
		return fmt.Errorf("no command %q on this surface", id)
	}
	inName, outName, err := resolveCapability(cmd.Capability)
	if err != nil {
		return err
	}
	reqMsg, err := newMessage(inName)
	if err != nil {
		return err
	}
	args := cmd.Args
	if len(args) == 0 {
		args = []byte("{}")
	}
	if err := protojson.Unmarshal(args, reqMsg); err != nil {
		return fmt.Errorf("command %q args: %w", id, err)
	}
	payload, err := proto.Marshal(reqMsg)
	if err != nil {
		return err
	}
	resp, err := gw.Invoke(ctx, &corev1.InvokeRequest{Capability: cmd.Capability, Payload: payload})
	if err != nil {
		return err // raw status (e.g. PermissionDenied if the consumer can't call it)
	}
	respMsg, err := newMessage(outName)
	if err != nil {
		return err
	}
	if err := proto.Unmarshal(resp.GetResult(), respMsg); err != nil {
		return err
	}
	b, _ := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(respMsg)
	fmt.Fprintf(out, "ran %q (%s):\n%s\n", id, cmd.Capability, b)
	return nil
}
