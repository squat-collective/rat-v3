package registry

import (
	"fmt"

	"github.com/rat-dev/rat/core/conformance"
	"github.com/rat-dev/rat/core/manifest"
)

// NewVerified is the D4 registry: like New, but a plugin's declared `provides` are
// trusted ONLY if a conformance attestation proves them — the core verifies
// `declared == conformed` instead of accepting the manifest's self-assertion
// (reviews/10 D4). For every manifest that provides any capability it requires an
// attestation (keyed by plugin name) that (a) verifies against the authority and
// (b) covers EVERY provided capability; a plugin that declares an unconformed
// capability, carries a bad signature, or has no attestation is REFUSED — it is not
// a valid provider ("declared but unconformed" is the gap C6 names). A pure
// caller/driver (no `provides`) needs no attestation.
//
// On success the verified manifests are handed to New unchanged, so the gateway's C5
// path is identical — it just can no longer be fed a self-asserted provider.
func NewVerified(manifests []*manifest.Manifest, attestations map[string]conformance.Attestation, authority *conformance.Authority) (*Registry, error) {
	if authority == nil {
		return nil, fmt.Errorf("NewVerified requires a conformance authority")
	}
	for _, m := range manifests {
		provides := m.ProvidesCaps()
		if len(provides) == 0 {
			continue // caller/driver: nothing to attest
		}
		name := m.Metadata.Name
		att, ok := attestations[name]
		if !ok {
			return nil, fmt.Errorf("plugin %q provides %d capabilities but has no conformance attestation (D4: declared but unconformed)", name, len(provides))
		}
		if att.PluginName != name {
			return nil, fmt.Errorf("conformance attestation for %q does not match manifest %q", att.PluginName, name)
		}
		if err := authority.Verify(att); err != nil {
			return nil, fmt.Errorf("plugin %q: %w", name, err)
		}
		for _, capURI := range provides {
			if !att.Conforms(capURI) {
				return nil, fmt.Errorf("plugin %q declares `provides` %q but it is not conformed (D4: declared != conformed)", name, capURI)
			}
			// Q02 PU-3: a conformed capability can be REVOKED out-of-band (e.g. a CVE
			// disclosed against that version) without rotating the authority key — it is
			// refused even though the signed attestation still covers it.
			if authority.IsRevoked(name, capURI) {
				return nil, fmt.Errorf("plugin %q: conformance for %q has been REVOKED (Q02 PU-3)", name, capURI)
			}
		}
	}
	return New(manifests)
}
