package main

// plugindev.go — `rat plugin dev` (backlog DX-7): the watch loop for plugin authoring.
// Polls the plugin dir (no fsnotify — the no-new-dependency discipline holds; a 1s
// mtime scan is plenty for a dev loop) and, on any change, re-runs the same gates the
// author would run by hand: `check` (instant, static) then `test` (image build + I9
// launch + smoke-invoke). Failures don't stop the loop — fix the file and save again.
//
//	rat plugin dev [<dir>] [--interval 1s] [--check-only]

import (
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// devSkipDirs are mutated-by-tooling dirs that must not retrigger the loop.
var devSkipDirs = map[string]bool{
	".git": true, ".rat": true, "__pycache__": true, "node_modules": true,
	"bin": true, "dist": true, ".venv": true,
}

// dirDigest fingerprints a tree: every file's path+size+mtime folded into one fnv64.
// Cheap (no content reads), deterministic, and changes whenever any watched file does.
func dirDigest(dir string) (uint64, error) {
	h := fnv.New64a()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if devSkipDirs[d.Name()] && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".pyc") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s|%d|%d\n", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return h.Sum64(), err
}

// runPluginDev implements the watch loop. It never returns on its own — Ctrl-C ends it
// (each `test` cycle launches and tears down its own sandbox, so there is nothing to
// drain here).
func runPluginDev(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat plugin dev", flag.ContinueOnError)
	interval := fs.Duration("interval", time.Second, "poll interval")
	checkOnly := fs.Bool("check-only", false, "re-run only the static gate (skip the image build + launch)")
	dir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Fprintf(out, "rat plugin dev — watching %s (every %s; Ctrl-C to stop)\n", dir, *interval)
	var last uint64
	for {
		d, err := dirDigest(dir)
		if err != nil {
			return fmt.Errorf("watch %s: %w", dir, err)
		}
		if d != last {
			last = d
			fmt.Fprintf(out, "\n── %s — change detected ──────────────────────────\n", time.Now().Format("15:04:05"))
			if err := runPluginCheck([]string{dir}, out); err != nil {
				fmt.Fprintf(out, "✗ check: %v\n   (fix + save — still watching)\n", err)
			} else if !*checkOnly {
				if err := runPluginTest([]string{dir}, out); err != nil {
					fmt.Fprintf(out, "✗ test: %v\n   (fix + save — still watching)\n", err)
				}
			}
			// The gates may have touched nothing in dir, but re-digest so our own
			// cycle's mtime effects (if any) don't immediately retrigger.
			if d2, err := dirDigest(dir); err == nil {
				last = d2
			}
		}
		time.Sleep(*interval)
	}
}
