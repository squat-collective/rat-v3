package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// dirDigest drives `rat plugin dev`'s change detection (DX-7): stable when nothing
// changes, different on any content/mtime change, blind to tooling litter.
func TestDirDigest(t *testing.T) {
	dir := t.TempDir()
	vWrite(t, filepath.Join(dir, "server.py"), "v1")
	vWrite(t, filepath.Join(dir, "manifest.yaml"), "kind: x")

	d1, err := dirDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := dirDigest(dir)
	if d1 != d2 {
		t.Fatal("digest must be stable when nothing changed")
	}

	// Content/mtime change → new digest. (Force a distinct mtime: coarse fs clocks
	// could otherwise give same size+mtime for an instant rewrite.)
	vWrite(t, filepath.Join(dir, "server.py"), "v2-changed")
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(filepath.Join(dir, "server.py"), future, future)
	d3, _ := dirDigest(dir)
	if d3 == d1 {
		t.Fatal("digest must change when a file changes")
	}

	// Tooling litter must NOT retrigger the loop.
	vWrite(t, filepath.Join(dir, "__pycache__", "junk.pyc"), "x")
	vWrite(t, filepath.Join(dir, ".rat", "daemon.log"), "x")
	d4, _ := dirDigest(dir)
	if d4 != d3 {
		t.Fatal("__pycache__/.rat litter must not change the digest")
	}

	// A new real file does.
	vWrite(t, filepath.Join(dir, "store.py"), "new")
	if d5, _ := dirDigest(dir); d5 == d4 {
		t.Fatal("a new source file must change the digest")
	}
}
