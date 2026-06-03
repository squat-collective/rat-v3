package client

import "testing"

// TestMatchesSurface covers the CLI surface consumer's filter (ADR-025): a component is
// shown on a surface if it targets that surface or is surface-agnostic.
func TestMatchesSurface(t *testing.T) {
	cases := []struct {
		compSurface string
		surface     string
		want        bool
	}{
		{"cli", "cli", true},
		{"vscode", "cli", false},   // the other surface's interface is invisible here
		{"", "cli", true},          // agnostic
		{"*", "cli", true},         // agnostic
		{"generic", "cli", true},   // agnostic
		{"cli", "vscode", false},
		{"webapp", "webapp", true},
	}
	for _, c := range cases {
		got := matchesSurface(uiComponent{Surface: c.compSurface}, c.surface)
		if got != c.want {
			t.Errorf("matchesSurface(surface=%q, want=%q) = %v, want %v", c.compSurface, c.surface, got, c.want)
		}
	}
}
