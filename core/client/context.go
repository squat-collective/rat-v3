package client

// context.go — `rat context`, a kubectl-style connection profile (ADR [[remote-dev-flow]]). It
// pins {addr, as, token, workspace} under a name so commands target a remote rat without retyping
// flags every call. The laptop→remote payoff: `rat context use prod` once, then `rat run`,
// `rat branch create x`, `rat call …` all hit the remote (over an SSH tunnel or a TLS hub).
//
//	rat context add prod --addr 127.0.0.1:7777 --as me     # e.g. an SSH-tunneled remote
//	rat context use prod
//	rat context list | show | remove <name>
//
// The current context supplies command flag DEFAULTS, so an explicit --addr/--as/etc. overrides it.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RatContext is one named connection profile.
type RatContext struct {
	Name      string `json:"name"`
	Addr      string `json:"addr"`
	As        string `json:"as,omitempty"`
	Token     string `json:"token,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type contextFile struct {
	Current  string       `json:"current"`
	Contexts []RatContext `json:"contexts"`
}

const localGateway = "127.0.0.1:7777"

func contextPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "rat", "contexts.json")
}

func loadContexts() contextFile {
	var cf contextFile
	if b, err := os.ReadFile(contextPath()); err == nil {
		_ = json.Unmarshal(b, &cf)
	}
	return cf
}

func saveContexts(cf contextFile) error {
	p := contextPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cf, "", "  ")
	return os.WriteFile(p, b, 0o600) // 0600 — may hold a token
}

// CurrentContext returns the active profile's connection defaults; addr falls back to the
// enclosing project's daemon socket (the `rat up` flow), then to the local TCP gateway.
// Commands use these as flag defaults (so an explicit flag still wins).
func CurrentContext() RatContext {
	cf := loadContexts()
	for _, c := range cf.Contexts {
		if c.Name == cf.Current {
			if c.Addr == "" {
				c.Addr = defaultGateway()
			}
			return c
		}
	}
	return RatContext{Addr: defaultGateway()}
}

// defaultGateway prefers the enclosing project's daemon socket: walk up from cwd to the
// dir holding rat.toml; if that project's daemon is up (.rat/daemon.sock exists), target
// it — so `rat call` works inside a project with zero flags, the same reachability-trust
// door `rat status`/`rat down` use. Outside a project (or daemon down) fall back to the
// local TCP gateway. A named context or an explicit --addr always wins (handled above).
func defaultGateway() string {
	dir, err := os.Getwd()
	if err != nil {
		return localGateway
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "rat.toml")); err == nil {
			sock := filepath.Join(dir, ".rat", "daemon.sock")
			if _, err := os.Stat(sock); err == nil {
				return "unix://" + sock
			}
			return localGateway
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return localGateway
		}
		dir = parent
	}
}

// RunContext implements `rat context <add|use|list|show|remove>`.
func RunContext(argv []string, out io.Writer) error {
	if len(argv) == 0 {
		return runContextList(loadContexts(), out)
	}
	sub, rest := argv[0], argv[1:]
	cf := loadContexts()
	switch sub {
	case "add", "set":
		if len(rest) == 0 || rest[0] == "" || rest[0][0] == '-' {
			return fmt.Errorf("usage: rat context add <name> --addr <host:port> [--as <caller>] [--token <t>] [--workspace <w>]")
		}
		name := rest[0]
		fs := flag.NewFlagSet("rat context add", flag.ContinueOnError)
		addr := fs.String("addr", localGateway, "gateway address (an SSH-tunneled local port, or a host:port / hub)")
		as := fs.String("as", "", "default caller identity")
		token := fs.String("token", "", "bearer token for an authenticating hub")
		ws := fs.String("workspace", "", "route via a hub to this workspace")
		if err := fs.Parse(rest[1:]); err != nil {
			return err
		}
		c := RatContext{Name: name, Addr: *addr, As: *as, Token: *token, Workspace: *ws}
		replaced := false
		for i := range cf.Contexts {
			if cf.Contexts[i].Name == name {
				cf.Contexts[i], replaced = c, true
			}
		}
		if !replaced {
			cf.Contexts = append(cf.Contexts, c)
		}
		if cf.Current == "" {
			cf.Current = name // first context becomes current
		}
		if err := saveContexts(cf); err != nil {
			return err
		}
		fmt.Fprintf(out, "context %q → %s%s (current: %s)\n", name, *addr, wsSuffix(*ws), cf.Current)
		return nil

	case "use", "switch":
		if len(rest) == 0 {
			return fmt.Errorf("usage: rat context use <name>")
		}
		for _, c := range cf.Contexts {
			if c.Name == rest[0] {
				cf.Current = rest[0]
				if err := saveContexts(cf); err != nil {
					return err
				}
				fmt.Fprintf(out, "now using context %q → %s\n", c.Name, c.Addr)
				return nil
			}
		}
		return fmt.Errorf("no context %q (rat context list)", rest[0])

	case "remove", "rm", "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: rat context remove <name>")
		}
		kept := cf.Contexts[:0]
		found := false
		for _, c := range cf.Contexts {
			if c.Name == rest[0] {
				found = true
				continue
			}
			kept = append(kept, c)
		}
		cf.Contexts = kept
		if cf.Current == rest[0] {
			cf.Current = ""
		}
		if !found {
			return fmt.Errorf("no context %q", rest[0])
		}
		if err := saveContexts(cf); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed context %q\n", rest[0])
		return nil

	case "show":
		c := CurrentContext()
		fmt.Fprintf(out, "current context: %s\n  addr:      %s\n  as:        %s\n  workspace: %s\n  token:     %s\n",
			orNone(cf.Current), c.Addr, orNone(c.As), orNone(c.Workspace), tokenMask(c.Token))
		return nil

	case "list", "ls":
		return runContextList(cf, out)
	}
	return fmt.Errorf("unknown `rat context %s` (add | use | list | show | remove)", sub)
}

func runContextList(cf contextFile, out io.Writer) error {
	if len(cf.Contexts) == 0 {
		fmt.Fprintln(out, "no contexts — `rat context add <name> --addr <host:port>`")
		return nil
	}
	for _, c := range cf.Contexts {
		marker := "  "
		if c.Name == cf.Current {
			marker = "* "
		}
		fmt.Fprintf(out, "%s%-14s %s%s%s\n", marker, c.Name, c.Addr, asSuffix(c.As), wsSuffix(c.Workspace))
	}
	return nil
}

func asSuffix(as string) string {
	if as == "" {
		return ""
	}
	return "  as=" + as
}
func wsSuffix(ws string) string {
	if ws == "" {
		return ""
	}
	return "  workspace=" + ws
}
func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
func tokenMask(t string) string {
	if t == "" {
		return "(none)"
	}
	return "(set)"
}
