package main

// capabilities.go — `rat capabilities` (backlog DX-3): the capability registry this rat
// links, readable. Before this, discovering valid `rat://` URIs meant scanning the axis
// proto for `(rat.common.v1.capability)` annotations or reading its CONTRACT.md table;
// the schema README calls the missing curated registry out explicitly. This verb renders
// the registry the binary already carries — the same linked descriptors `rat plugin
// check` and the gateway route by — so it can never drift from what this rat enforces.
//
//	rat capabilities                  # every capability, grouped by axis
//	rat capabilities state            # one axis…
//	rat capabilities state-backend    # …or its plugin kind (same thing)

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	commonv1 "github.com/le-squat/rat/gen/rat/common/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// capInfo is one annotated RPC: the capability URI and the wire facts an author needs.
type capInfo struct {
	URI, Axis, Method, In, Out, Cardinality string
}

// allCapabilities enumerates every `(rat.common.v1.capability)`-annotated method in the
// linked descriptors, sorted by URI. This is the authoritative in-binary registry.
func allCapabilities() []capInfo {
	var caps []capInfo
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			ms := svcs.Get(i).Methods()
			for j := 0; j < ms.Len(); j++ {
				m := ms.Get(j)
				uri, _ := proto.GetExtension(m.Options(), commonv1.E_Capability).(string)
				if uri == "" {
					continue
				}
				card := "unary"
				switch {
				case m.IsStreamingServer() && m.IsStreamingClient():
					card = "bidi-stream"
				case m.IsStreamingServer():
					card = "server-stream"
				case m.IsStreamingClient():
					card = "client-stream"
				}
				caps = append(caps, capInfo{
					URI: uri, Axis: capAxisOf(uri), Method: string(m.Name()),
					In: string(m.Input().Name()), Out: string(m.Output().Name()),
					Cardinality: card,
				})
			}
		}
		return true
	})
	sort.Slice(caps, func(i, j int) bool { return caps[i].URI < caps[j].URI })
	return caps
}

// axisOfKind resolves a kind to its axis: the kindToAxis map (plugin.go) for the renamed
// kinds, else the kind IS the axis.
func axisOfKind(kind string) string {
	if a, ok := kindToAxis[kind]; ok {
		return a
	}
	return kind
}

// axisKind maps a URI axis segment back to the plugin kind that serves it ("" when no
// known kind does — e.g. a community axis this rat merely links). NOTE the frozen-wire
// wart: deployment-runtime URIs keep the hyphen (rat://deployment-runtime/v1/launch)
// while its proto DIRECTORY is deploymentruntime/ — so match the kind name itself too.
func axisKind(axis string) string {
	for kind := range knownKinds {
		if kind == axis || axisOfKind(kind) == axis {
			return kind
		}
	}
	return ""
}

// runCapabilities implements `rat capabilities [<axis>|<kind>]`.
func runCapabilities(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat capabilities", flag.ContinueOnError)
	filter := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		filter, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	byAxis := map[string][]capInfo{}
	for _, c := range allCapabilities() {
		byAxis[c.Axis] = append(byAxis[c.Axis], c)
	}
	axes := make([]string, 0, len(byAxis))
	for a := range byAxis {
		axes = append(axes, a)
	}
	sort.Strings(axes)

	// A kind filter resolves to its URI axis segment — which for deployment-runtime is
	// the kind name itself (the frozen wire kept the hyphen; the proto dir didn't).
	axisFilter := filter
	if filter != "" && knownKinds[filter] {
		if mapped := axisOfKind(filter); byAxis[mapped] != nil {
			axisFilter = mapped
		}
	}

	if axisFilter != "" {
		if byAxis[axisFilter] == nil {
			return fmt.Errorf("no axis %q linked into this rat — axes: %s (a kind like %q works too)",
				axisFilter, strings.Join(axes, " "), "state-backend")
		}
		axes = []string{axisFilter}
	}

	total := 0
	for _, axis := range axes {
		kindNote := ""
		if k := axisKind(axis); k != "" {
			kindNote = " · kind: " + k
		}
		// The proto dir drops hyphens (deployment-runtime URIs live in deploymentruntime/).
		fmt.Fprintf(out, "%s%s · contracts/proto/rat/%s/v1/CONTRACT.md\n",
			axis, kindNote, strings.ReplaceAll(axis, "-", ""))
		for _, c := range byAxis[axis] {
			fmt.Fprintf(out, "  %-42s %-16s %-13s %s → %s\n", c.URI, c.Method, c.Cardinality, c.In, c.Out)
			total++
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "%d capabilit%s across %d ax%s (the registry compiled into this rat — what `rat plugin check` and the gateway enforce)\n",
		total, plural(total), len(axes), pluralEs(len(axes)))
	return nil
}

func pluralEs(n int) string {
	if n == 1 {
		return "is"
	}
	return "es"
}
