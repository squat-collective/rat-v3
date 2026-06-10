// main.go — runs the benchmark + prints the report.
//
//	go run .            # default N=20000
//	go run . 50000      # custom iteration count
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"time"

	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
	corev1 "github.com/squat-collective/rat-v3/gen/rat/core/v1"
	runtimev1 "github.com/squat-collective/rat-v3/gen/rat/runtime/v1"
	statev1 "github.com/squat-collective/rat-v3/gen/rat/state/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

func dial(addr string) *grpc.ClientConn {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial %s: %v", addr, err)
	}
	return conn
}

// withCallMeta builds the rat-callmeta-bin envelope a calling plugin's SDK attaches
// on every mediated call (ADR-007) — its marshal cost is part of the mediated path.
func withCallMeta(ctx context.Context) context.Context {
	rc := &commonv1.RequestContext{
		Trace:    &commonv1.TraceContext{Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", CorrelationId: "corr-bench"},
		Identity: &commonv1.Identity{Tenant: "acme"},
	}
	b, _ := proto.Marshal(rc)
	return metadata.AppendToOutgoingContext(ctx, callMetaHeader, string(b))
}

func measure(n int, fn func()) (p50, p99, mean time.Duration) {
	d := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		t := time.Now()
		fn()
		d[i] = time.Since(t)
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	var sum time.Duration
	for _, x := range d {
		sum += x
	}
	return d[n/2], d[(n*99)/100], sum / time.Duration(n)
}

func main() {
	n := 20000
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil && v > 0 {
			n = v
		}
	}

	// plugin (serves both StateService.Get + RuntimeService.Execute)
	plis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	psrv := grpc.NewServer()
	pl := &plugin{}
	statev1.RegisterStateServiceServer(psrv, pl)
	runtimev1.RegisterRuntimeServiceServer(psrv, pl)
	go func() { _ = psrv.Serve(plis) }()
	defer psrv.Stop()
	providerConn := dial(plis.Addr().String())

	// gateway, pointed at the plugin
	glis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	gsrv := grpc.NewServer()
	corev1.RegisterCapabilityInvokeServiceServer(gsrv, newGateway(providerConn))
	go func() { _ = gsrv.Serve(glis) }()
	defer gsrv.Stop()
	gwClient := corev1.NewCapabilityInvokeServiceClient(dial(glis.Addr().String()))

	stateClient := statev1.NewStateServiceClient(providerConn)
	runtimeClient := runtimev1.NewRuntimeServiceClient(providerConn)
	ctx := context.Background()

	getPayload, _ := proto.Marshal(&statev1.GetRequest{Key: "bench/key"})
	execPayload, _ := proto.Marshal(&runtimev1.ExecuteRequest{WorkSpec: []byte("bench")})

	scenarios := []struct {
		name string
		fn   func()
	}{
		{"unary  state.Get        direct", func() { _, _ = stateClient.Get(ctx, &statev1.GetRequest{Key: "bench/key"}) }},
		{"unary  state.Get        mediated", func() {
			out, _ := gwClient.Invoke(withCallMeta(ctx), &corev1.InvokeRequest{Capability: "rat://state/v1/get", Payload: getPayload})
			var resp statev1.GetResponse
			_ = proto.Unmarshal(out.GetResult(), &resp)
		}},
		{"stream runtime.Execute   direct", func() {
			s, _ := runtimeClient.Execute(ctx, &runtimev1.ExecuteRequest{WorkSpec: []byte("bench")})
			for {
				if _, err := s.Recv(); err != nil {
					break
				}
			}
		}},
		{"stream runtime.Execute   mediated", func() {
			s, _ := gwClient.InvokeServerStream(withCallMeta(ctx), &corev1.InvokeServerStreamRequest{Capability: "rat://runtime/v1/execute", Payload: execPayload})
			for {
				if _, err := s.Recv(); err != nil {
					break
				}
			}
		}},
	}

	// warmup (establish connections, warm caches)
	for _, s := range scenarios {
		for i := 0; i < 500; i++ {
			s.fn()
		}
	}

	type row struct {
		name             string
		p50, p99, mean   time.Duration
	}
	rows := make([]row, len(scenarios))
	for i, s := range scenarios {
		p50, p99, mean := measure(n, s.fn)
		rows[i] = row{s.name, p50, p99, mean}
	}

	fmt.Printf("\nRAT per-RPC latency benchmark — N=%d, localhost TCP, single goroutine\n", n)
	fmt.Println("(quantifies the ADR-005 core-mediation hop + the ADR-008 streaming relay)")
	fmt.Println("======================================================================")
	fmt.Printf("  %-34s %10s %10s %10s\n", "scenario", "p50", "p99", "mean")
	fmt.Println("  ---------------------------------- ---------- ---------- ----------")
	for _, r := range rows {
		fmt.Printf("  %-34s %10s %10s %10s\n", r.name, us(r.p50), us(r.p99), us(r.mean))
	}
	fmt.Println("  ---------------------------------- ---------- ---------- ----------")
	overhead("unary  state.Get", rows[0].p50, rows[1].p50)
	overhead("stream runtime.Execute", rows[2].p50, rows[3].p50)
	fmt.Println("\nNote: the mediated path also marshals the request + unmarshals the result")
	fmt.Println("client-side (the SDK's job) and stamps a fresh rat-callmeta-bin envelope —")
	fmt.Println("all included above. Bulk DATA bypasses this path entirely (ArrowStream).")
}

func overhead(label string, direct, mediated time.Duration) {
	delta := mediated - direct
	pct := float64(delta) / float64(direct) * 100
	fmt.Printf("  → %-24s gateway overhead (p50): +%s (+%.0f%%)\n", label, us(delta), pct)
}

func us(d time.Duration) string {
	return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000)
}
