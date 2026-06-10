// Command ratctl is the standalone client alias for a running `rat serve` orchestrator.
// It is a thin shim over the shared client logic (core/client) that ALSO backs `rat call`
// / `rat apply` (ADR-023 folds the client into the one `rat` binary; ratctl stays as a
// transition alias). The orchestrator/client boundary (ADR-019) remains a real separation
// — this is just the other entrypoint to the same code.
//
//	ratctl call <capability> --as <caller> [--data '<protojson>'] [--addr host:port]
//	ratctl apply --project <dir> --name <name>
package main

import (
	"fmt"
	"os"

	"github.com/squat-collective/rat-v3/core/client"
)

func main() {
	if err := client.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ratctl:", err)
		os.Exit(1)
	}
}
