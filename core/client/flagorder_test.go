package client

import (
	"flag"
	"io"
	"testing"
)

// DX-9: `rat call --as dev rat://…` and `rat call rat://… --as dev` must both parse —
// the platform README shipped the flags-first order for weeks while it silently didn't.
func TestParseWithPositionalAnyOrder(t *testing.T) {
	mk := func() (*flag.FlagSet, *string) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		as := fs.String("as", "", "")
		fs.String("data", "{}", "")
		return fs, as
	}

	for name, args := range map[string][]string{
		"flags-first":  {"--as", "dev", "rat://state/v1/get"},
		"flags-after":  {"rat://state/v1/get", "--as", "dev"},
		"interspersed": {"--as", "dev", "rat://state/v1/get", "--data", "{}"},
	} {
		fs, as := mk()
		pos, err := parseWithPositional(fs, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if pos != "rat://state/v1/get" || *as != "dev" {
			t.Fatalf("%s: pos=%q as=%q", name, pos, *as)
		}
	}

	fs, _ := mk()
	if pos, err := parseWithPositional(fs, []string{"--as", "dev"}); err != nil || pos != "" {
		t.Fatalf("no positional: pos=%q err=%v", pos, err)
	}

	fs, _ = mk()
	if _, err := parseWithPositional(fs, []string{"rat://a", "rat://b"}); err == nil {
		t.Fatal("two positionals must error")
	}
}
