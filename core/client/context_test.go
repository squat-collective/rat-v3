package client

import (
	"os"
	"path/filepath"
	"testing"
)

// defaultGateway (the zero-flag address resolution behind `rat call`/`rat apply`) must
// prefer the enclosing project's daemon socket and fall back to the local TCP gateway —
// otherwise the documented project flow (`rat init && rat up && rat call …`) dials
// 127.0.0.1:7777 while the daemon listens on .rat/daemon.sock.
func TestDefaultGatewayPrefersProjectSocket(t *testing.T) {
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "rat.toml"), []byte("name = \"t\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(proj)
	if got := defaultGateway(); got != localGateway {
		t.Fatalf("project without a running daemon: got %q, want %q", got, localGateway)
	}

	if err := os.MkdirAll(filepath.Join(proj, ".rat"), 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(proj, ".rat", "daemon.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	want := "unix://" + sock
	if got := defaultGateway(); got != want {
		t.Fatalf("project with daemon up: got %q, want %q", got, want)
	}

	nested := filepath.Join(proj, "deep", "subdir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if got := defaultGateway(); got != want {
		t.Fatalf("nested dir should walk up to the project socket: got %q, want %q", got, want)
	}

	t.Chdir(t.TempDir())
	if got := defaultGateway(); got != localGateway {
		t.Fatalf("outside a project: got %q, want %q", got, localGateway)
	}
}
