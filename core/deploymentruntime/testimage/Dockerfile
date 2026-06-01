# Test image for the podman deployment-runtime LIVE full-profile proof (ADR-016 §4).
#
# It needs podman AND the Go toolchain in one image, so `go test` can drive a REAL
# `podman run` (nested) and assert the kernel enforced the full I9 profile. Built and
# run (privileged) only by `make core-test-podman`; the app never uses it at runtime.
#
# The Go toolchain is copied from the official golang image (pinned 1.25) rather than
# dnf-installed, so the version exactly matches `core/go.mod` (GOTOOLCHAIN=local, no
# download). The `go` command is a static Go binary, so the cross-distro copy is safe;
# the podman test runs with CGO_ENABLED=0, so no C toolchain is needed.
FROM quay.io/podman/stable

COPY --from=docker.io/library/golang:1.25 /usr/local/go /usr/local/go

ENV PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
