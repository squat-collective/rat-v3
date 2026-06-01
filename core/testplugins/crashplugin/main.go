// Command crashplugin is a launch target that CRASHES immediately (exits non-zero
// without ever binding its endpoint) — so the deployment-runtime reports it unhealthy
// and the reconciler must back its restarts off and eventually mark it Degraded
// (sre#4 crash-loop discipline). Not a production plugin — a spike test target.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "crashplugin: crashing on purpose (never becomes healthy)")
	os.Exit(1)
}
