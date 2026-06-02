// Context-carriage conformance — Go reference (PU-2, ADR-017).
//
// One of TWO technologically-divergent reference implementations (Go + Python) of the
// KEYSTONE context-carriage contract (common/v1/context.proto + ADR-007 gateway stamping):
// the rules a core gateway / consuming hop MUST apply on each hop. Both impls cross-run the
// SHARED golden vectors (../context-carriage-v1.json) — the ADR-003 two-reference forcing
// function applied to the most-irreversible frozen surface, which the data-axis conformance
// skipped (architect F1 / maintainer-conceded, reviews/11-q02-architect.md).
//
// This impl is a CLEAN-ROOM reference of the contract from context.proto's prose — it shares
// no code with core/gateway. If the two impls disagree on any vector, the contract is
// under-specified; agreement is the conformance signal. stdlib only (no SDK dependency): the
// suite conforms the stamping LOGIC (re-stamp vs propagate, caller re-derivation, the M4
// bare-mirror cross-check, subject verification), not the proto wire-bytes (those are the
// shared, now-connectionless, codegen's job — ADR-018).
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ---- logical envelope (mirrors common/v1/context.proto) -----------------------------

type traceContext struct {
	Traceparent   string
	Tracestate    string
	CorrelationID string
}

type subjectAssertion struct {
	Principal          string // the BARE mirror of the signed principal
	Signature          []byte
	BoundCorrelationID string
	ExpiresUnixMs      int64
	KeyID              string
}

type identity struct {
	CallerPlugin string
	Tenant       string // the BARE mirror of the signed tenant
	Subject      *subjectAssertion
}

type requestContext struct {
	Trace          *traceContext
	Identity       *identity
	DeadlineUnixMs int64
}

// ---- the keystone contract ----------------------------------------------------------

// canonicalSubject is the deterministic byte string the core signs and a hop reconstructs
// FROM THE BARE MIRRORS to verify (context.proto SubjectAssertion VERIFICATION CONTRACT).
// Because it is rebuilt from the bare mirrors, a bare principal/tenant the signature does
// not cover fails verification — that IS the M4 cross-check (step 4), enforced structurally.
func canonicalSubject(principal, tenant, bound string, expires int64, keyID string) []byte {
	return []byte(principal + "\x00" + tenant + "\x00" + bound + "\x00" + strconv.FormatInt(expires, 10) + "\x00" + keyID)
}

func wellFormedTraceparent(tp string) bool {
	p := strings.Split(tp, "-")
	return len(p) == 4 && len(p[0]) == 2 && len(p[1]) == 32 && len(p[2]) == 16 && len(p[3]) == 2
}

// stamp applies the per-hop context-carriage contract: validate the inbound envelope and
// either REJECT (returning a reason code) or RE-STAMP a fresh downstream RequestContext.
// channelCaller is THIS hop's authenticated caller (C2) — caller_plugin is re-derived from
// it, never propagated from the inbound envelope.
func stamp(in *requestContext, channelCaller string, keyring map[string]ed25519.PublicKey, nowMs int64) (down *requestContext, principal, reject string) {
	// C1 — traceparent + correlation_id mandatory.
	if in.Trace == nil || !wellFormedTraceparent(in.Trace.Traceparent) {
		return nil, "", "traceparent"
	}
	if in.Trace.CorrelationID == "" {
		return nil, "", "correlation"
	}
	// SUBJECT — verify the core-signed assertion if present (else bootstrap/pre-auth).
	if s := in.Identity.Subject; s != nil {
		if s.BoundCorrelationID != in.Trace.CorrelationID { // anti-stockpile
			return nil, "", "subject-bound"
		}
		if nowMs > s.ExpiresUnixMs { // short-TTL
			return nil, "", "subject-expired"
		}
		pub, ok := keyring[s.KeyID]
		if !ok {
			return nil, "", "subject-signature"
		}
		// M4: reconstruct from the BARE mirrors (s.Principal + in.Identity.Tenant).
		if !ed25519.Verify(pub, canonicalSubject(s.Principal, in.Identity.Tenant, s.BoundCorrelationID, s.ExpiresUnixMs, s.KeyID), s.Signature) {
			return nil, "", "subject-signature"
		}
		principal = s.Principal
	}
	// RE-STAMP — trace verbatim; identity server-controlled (caller re-derived).
	down = &requestContext{
		Trace:          in.Trace,
		Identity:       &identity{CallerPlugin: channelCaller, Tenant: in.Identity.Tenant, Subject: in.Identity.Subject},
		DeadlineUnixMs: in.DeadlineUnixMs,
	}
	return down, principal, ""
}

// ---- vectors ------------------------------------------------------------------------

type vectors struct {
	Signing struct {
		SeedHex string `json:"seed_hex"`
		KeyID   string `json:"key_id"`
	} `json:"signing"`
	Cases []struct {
		Name          string `json:"name"`
		ChannelCaller string `json:"channel_caller"`
		NowUnixMs     int64  `json:"now_unix_ms"`
		Inbound       struct {
			Traceparent    string `json:"traceparent"`
			Tracestate     string `json:"tracestate"`
			CorrelationID  string `json:"correlation_id"`
			CallerPlugin   string `json:"caller_plugin"`
			Tenant         string `json:"tenant"`
			DeadlineUnixMs int64  `json:"deadline_unix_ms"`
			Subject        *struct {
				Signed struct {
					Principal          string `json:"principal"`
					Tenant             string `json:"tenant"`
					BoundCorrelationID string `json:"bound_correlation_id"`
					ExpiresUnixMs      int64  `json:"expires_unix_ms"`
					KeyID              string `json:"key_id"`
				} `json:"signed"`
				BarePrincipal string `json:"bare_principal"`
				Signature     string `json:"signature"` // "valid" | "tampered"
			} `json:"subject"`
		} `json:"inbound"`
		Expect struct {
			Outcome    string `json:"outcome"`
			Reason     string `json:"reason"`
			Downstream *struct {
				Traceparent      string `json:"traceparent"`
				Tracestate       string `json:"tracestate"`
				CorrelationID    string `json:"correlation_id"`
				CallerPlugin     string `json:"caller_plugin"`
				Tenant           string `json:"tenant"`
				SubjectPrincipal string `json:"subject_principal"`
			} `json:"downstream"`
		} `json:"expect"`
	} `json:"cases"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: context-carriage-go <vectors.json>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read vectors:", err)
		os.Exit(2)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		fmt.Fprintln(os.Stderr, "parse vectors:", err)
		os.Exit(2)
	}
	seed, err := hex.DecodeString(v.Signing.SeedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintln(os.Stderr, "bad signing seed")
		os.Exit(2)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	keyring := map[string]ed25519.PublicKey{v.Signing.KeyID: priv.Public().(ed25519.PublicKey)}

	fails := 0
	for _, c := range v.Cases {
		in := &requestContext{
			Trace:          &traceContext{Traceparent: c.Inbound.Traceparent, Tracestate: c.Inbound.Tracestate, CorrelationID: c.Inbound.CorrelationID},
			Identity:       &identity{CallerPlugin: c.Inbound.CallerPlugin, Tenant: c.Inbound.Tenant},
			DeadlineUnixMs: c.Inbound.DeadlineUnixMs,
		}
		if s := c.Inbound.Subject; s != nil {
			sig := ed25519.Sign(priv, canonicalSubject(s.Signed.Principal, s.Signed.Tenant, s.Signed.BoundCorrelationID, s.Signed.ExpiresUnixMs, s.Signed.KeyID))
			if s.Signature == "tampered" {
				sig[0] ^= 0xff
			}
			in.Identity.Subject = &subjectAssertion{
				Principal:          s.BarePrincipal,
				Signature:          sig,
				BoundCorrelationID: s.Signed.BoundCorrelationID,
				ExpiresUnixMs:      s.Signed.ExpiresUnixMs,
				KeyID:              s.Signed.KeyID,
			}
		}

		down, principal, reject := stamp(in, c.ChannelCaller, keyring, c.NowUnixMs)
		ok, detail := checkExpect(c.Expect, down, principal, reject)
		status := "PASS"
		if !ok {
			status, fails = "FAIL", fails+1
		}
		fmt.Printf("  [%s] %-40s %s\n", status, c.Name, detail)
	}
	fmt.Printf("context-carriage (go): %d/%d vectors pass\n", len(v.Cases)-fails, len(v.Cases))
	if fails > 0 {
		os.Exit(1)
	}
}

func checkExpect(exp struct {
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason"`
	Downstream *struct {
		Traceparent      string `json:"traceparent"`
		Tracestate       string `json:"tracestate"`
		CorrelationID    string `json:"correlation_id"`
		CallerPlugin     string `json:"caller_plugin"`
		Tenant           string `json:"tenant"`
		SubjectPrincipal string `json:"subject_principal"`
	} `json:"downstream"`
}, down *requestContext, principal, reject string) (bool, string) {
	if exp.Outcome == "reject" {
		if reject == "" {
			return false, "want reject, got accept"
		}
		if exp.Reason != "" && reject != exp.Reason {
			return false, fmt.Sprintf("reject reason %q, want %q", reject, exp.Reason)
		}
		return true, "reject:" + reject
	}
	// accept
	if reject != "" {
		return false, "want accept, got reject:" + reject
	}
	d := exp.Downstream
	switch {
	case down.Trace.Traceparent != d.Traceparent:
		return false, "traceparent not propagated verbatim"
	case down.Trace.Tracestate != d.Tracestate:
		return false, "tracestate not propagated verbatim"
	case down.Trace.CorrelationID != d.CorrelationID:
		return false, "correlation_id changed"
	case down.Identity.CallerPlugin != d.CallerPlugin:
		return false, fmt.Sprintf("caller_plugin=%q, want re-derived %q", down.Identity.CallerPlugin, d.CallerPlugin)
	case down.Identity.Tenant != d.Tenant:
		return false, "tenant not propagated"
	case principal != d.SubjectPrincipal:
		return false, fmt.Sprintf("subject principal=%q, want %q", principal, d.SubjectPrincipal)
	}
	return true, "accept"
}
