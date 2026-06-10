package main

// lifecycle.go — the daemon lifecycle verbs (ADR-023 slice 2c): `rat up [-d]`, `rat down`,
// `rat status`, `rat ls`. A project's daemon writes its pid to `.rat/daemon.pid` and an
// entry in the machine-global instance registry (~/.local/state/rat/instances.json) on
// start, and removes both on drain — so the CLI can find, inspect, and stop it. The daemon
// stays config-stateless; this is just where the RUNNING ones are tracked (status, not spec).

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// instanceEntry is one running daemon in the global registry (keyed by project Dir).
type instanceEntry struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
	Pid  int    `json:"pid"`
	Addr string `json:"addr"`
}

func registryDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "rat")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rat")
}
func registryFile() string { return filepath.Join(registryDir(), "instances.json") }

func loadRegistry() []instanceEntry {
	data, err := os.ReadFile(registryFile())
	if err != nil {
		return nil
	}
	var es []instanceEntry
	_ = json.Unmarshal(data, &es)
	return es
}

func saveRegistry(es []instanceEntry) {
	if err := os.MkdirAll(registryDir(), 0o755); err != nil {
		return
	}
	data, _ := json.MarshalIndent(es, "", "  ")
	_ = os.WriteFile(registryFile(), data, 0o644)
}

// pruneRegistry drops entries whose process is no longer alive (best-effort cleanup).
func pruneRegistry(es []instanceEntry) []instanceEntry {
	out := make([]instanceEntry, 0, len(es))
	for _, e := range es {
		if pidAlive(e.Pid) {
			out = append(out, e)
		}
	}
	return out
}

func registryUpsert(e instanceEntry) {
	es := pruneRegistry(loadRegistry())
	out := es[:0]
	for _, x := range es {
		if x.Dir != e.Dir {
			out = append(out, x)
		}
	}
	out = append(out, e)
	saveRegistry(out)
}

func registryRemove(dir string) {
	es := loadRegistry()
	out := make([]instanceEntry, 0, len(es))
	for _, x := range es {
		if x.Dir != dir {
			out = append(out, x)
		}
	}
	saveRegistry(out)
}

// pidAlive reports whether a pid is a live process (kill -0; EPERM still means alive).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// runningPid reads .rat/daemon.pid and reports whether that daemon is alive.
func runningPid(runtimeDir string) (int, bool) {
	data, err := os.ReadFile(filepath.Join(runtimeDir, "daemon.pid"))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return pid, pidAlive(pid)
}

// registerDaemon / deregisterDaemon are called by serveResolved (when the plane has a
// RuntimeDir, i.e. it's a project) to publish + retract this daemon's pid + registry entry.
func registerDaemon(pl *Plane) {
	if pl.RuntimeDir == "" {
		return
	}
	_ = os.MkdirAll(pl.RuntimeDir, 0o755)
	_ = os.WriteFile(filepath.Join(pl.RuntimeDir, "daemon.pid"), []byte(strconv.Itoa(os.Getpid())), 0o644)
	registryUpsert(instanceEntry{Name: pl.Instance, Dir: filepath.Dir(pl.RuntimeDir), Pid: os.Getpid(), Addr: pl.Addr})
}

func deregisterDaemon(pl *Plane) {
	if pl.RuntimeDir == "" {
		return
	}
	_ = os.Remove(filepath.Join(pl.RuntimeDir, "daemon.pid"))
	registryRemove(filepath.Dir(pl.RuntimeDir))
}

// --- verbs -----------------------------------------------------------------------------

// runUp discovers this project's rat.toml (walking up from cwd) and runs its daemon. With
// -d it spawns a detached background daemon (logs → .rat/daemon.log) and returns once it is
// serving; otherwise it runs in the foreground. It refuses to start a second daemon for a
// project that already has one (the unix socket would otherwise be silently hijacked).
func runUp(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rat up", flag.ContinueOnError)
	detach := fs.Bool("d", false, "run the daemon in the background")
	strict := fs.Bool("strict", false, "preflight the project (rat validate) and refuse to boot on any error")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tomlPath, dir, err := findProject(".")
	if err != nil {
		return err
	}
	if pid, alive := runningPid(filepath.Join(dir, ".rat")); alive {
		return fmt.Errorf("a daemon is already running for this project (pid %d) — `rat down` first", pid)
	}
	pl, err := LoadProject(tomlPath)
	if err != nil {
		return err
	}
	// The preflight is static, so it runs in THIS process even for -d (the detached
	// child re-execs a plain `rat up` — by then the plane is already validated).
	if *strict {
		if err := strictPreflight(pl, tomlPath); err != nil {
			return err
		}
	}
	if *detach {
		pid, err := spawnDetached(dir)
		if err != nil {
			return err
		}
		if err := waitReady(dir, pid); err != nil {
			return err
		}
		fmt.Fprintf(out, "rat up: daemon started in background (pid %d) — `rat status` / `rat down`\n", pid)
		return nil
	}
	return serveResolved(pl)
}

// spawnDetached starts `rat up` (foreground) as a detached child in its own process group,
// with output redirected to .rat/daemon.log. The child writes its own pid + registry entry.
func spawnDetached(dir string) (int, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	rd := filepath.Join(dir, ".rat")
	if err := os.MkdirAll(rd, 0o755); err != nil {
		return 0, err
	}
	logf, err := os.OpenFile(filepath.Join(rd, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	defer logf.Close()
	cmd := exec.Command(self, "up")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // survive the parent shell
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// waitReady polls the background daemon's log until it reports serving (or it dies / times out).
func waitReady(dir string, pid int) error {
	logPath := filepath.Join(dir, ".rat", "daemon.log")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(logPath); strings.Contains(string(b), "gateway serving") {
			return nil
		}
		if !pidAlive(pid) {
			b, _ := os.ReadFile(logPath)
			return fmt.Errorf("daemon exited during startup; .rat/daemon.log:\n%s", tailString(string(b), 1200))
		}
		time.Sleep(200 * time.Millisecond)
	}
	b, _ := os.ReadFile(logPath)
	return fmt.Errorf("daemon did not become ready in time; .rat/daemon.log:\n%s", tailString(string(b), 1200))
}

// runDown stops this project's background daemon (SIGTERM → drain) and waits for it to exit.
func runDown(args []string, out io.Writer) error {
	_, dir, err := findProject(".")
	if err != nil {
		return err
	}
	pid, alive := runningPid(filepath.Join(dir, ".rat"))
	if !alive {
		registryRemove(dir) // clear any stale entry
		fmt.Fprintln(out, "rat down: no daemon running for this project")
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal pid %d: %w", pid, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			fmt.Fprintf(out, "rat down: stopped (pid %d)\n", pid)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("daemon (pid %d) did not exit within the drain window", pid)
}

// runStatus reports this project's daemon state + its declared plugins.
func runStatus(args []string, out io.Writer) error {
	tomlPath, dir, err := findProject(".")
	if err != nil {
		return err
	}
	rt, err := parseProject(tomlPath)
	if err != nil {
		return err
	}
	pid, alive := runningPid(filepath.Join(dir, ".rat"))
	state := "stopped"
	if alive {
		state = fmt.Sprintf("running (pid %d)", pid)
	}
	fmt.Fprintf(out, "project %q — %s\n", orDefault(rt.Name, filepath.Base(dir)), state)
	if alive {
		fmt.Fprintf(out, "  socket: %s\n", filepath.Join(dir, ".rat", "daemon.sock"))
	}
	fmt.Fprintf(out, "  plugins (%d):\n", len(rt.Plugins))
	for _, p := range rt.Plugins {
		kind := "driver"
		if p.Image != "" {
			kind = p.Image
		}
		fmt.Fprintf(out, "    - %s (%s)\n", p.Name, kind)
	}
	return nil
}

// runLs lists every rat daemon running on this machine (the docker-ps of daemons).
func runLs(args []string, out io.Writer) error {
	es := pruneRegistry(loadRegistry())
	saveRegistry(es) // persist the prune
	if len(es) == 0 {
		fmt.Fprintln(out, "no rat daemons running")
		return nil
	}
	fmt.Fprintf(out, "%-20s %-8s %s\n", "NAME", "PID", "DIR")
	for _, e := range es {
		fmt.Fprintf(out, "%-20s %-8d %s\n", e.Name, e.Pid, e.Dir)
	}
	return nil
}

// tailString returns the last n bytes of s (for surfacing a daemon's recent log on error).
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
