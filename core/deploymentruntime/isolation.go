package deploymentruntime

import (
	deploymentruntimev1 "github.com/squat-collective/rat-v3/contracts/sdks/go/rat/deploymentruntime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// checkI9Minimum is the SHARED I9 trust gate every runtime in this package applies
// (the Go twin of the Python references' check_spec): an image is required, and the
// IsolationProfile MUST meet the I9 minimum — run_as_non_root + drop_all_capabilities
// + no_new_privileges. A spec that fails is refused. This is the trust boundary the
// whole "install many 3rd-party plugins" bet leans on (deployment_runtime.proto
// SECURITY / reviews/04 I9), so it is identical across runtimes — local-process can
// only honor the process-level subset, podman enforces the full profile, but both
// REFUSE below the minimum.
func checkI9Minimum(spec *deploymentruntimev1.LaunchSpec) error {
	if spec.GetImage() == "" {
		return status.Error(codes.InvalidArgument, "launch: spec.image is required")
	}
	iso := spec.GetIsolation()
	if !iso.GetRunAsNonRoot() || !iso.GetDropAllCapabilities() || !iso.GetNoNewPrivileges() {
		return status.Error(codes.FailedPrecondition,
			"I9: isolation profile is below the minimum (run_as_non_root + drop_all_capabilities + no_new_privileges all required)")
	}
	return nil
}

// isolationReceipt is the JSON shape CONTRACT.md mandates in Healthcheck.detail so
// the honored profile is observable (the conformance stand-in for "the runtime
// actually applied the profile"). A REAL enforcing runtime (podman) sets the five
// bools to what the kernel actually applied — unlike the v1 references, which
// self-attest read_only_root_fs while enforcing nothing (the reviews/08 D1 honesty
// gap this package's podman runtime closes).
type isolationReceipt struct {
	Kind             string         `json:"kind"`
	IsolationHonored honoredProfile `json:"isolation_honored"`
	SeccompProfile   string         `json:"seccomp_profile,omitempty"`
}

type honoredProfile struct {
	RunAsNonRoot        bool `json:"run_as_non_root"`
	DropAllCapabilities bool `json:"drop_all_capabilities"`
	NoNewPrivileges     bool `json:"no_new_privileges"`
	ReadOnlyRootFs      bool `json:"read_only_root_fs"`
	BlockMetadataEgress bool `json:"block_metadata_egress"`
}
