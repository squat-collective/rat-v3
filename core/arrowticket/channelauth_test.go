package arrowticket

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
)

// Q02 PU-1 (ADR-017) — the bytes-leg producer-channel-auth MUST (common/v1/data.proto
// ArrowStream.ticket). The ticket binds {caller,tenant}, but because the bulk Arrow leg
// BYPASSES the core, that binding is only as strong as the channel the producer checks it
// against. bulkleg_test.go's producer trusts request HEADERS (the spike stand-in) — so a
// leaked ticket replayed with spoofed X-RAT-* headers would succeed (proven below as the
// gap). A CONFORMANT producer MUST derive the presenting identity from the AUTHENTICATED
// CHANNEL (here: the mTLS client certificate; in production mTLS or a core-vended channel
// token), never from app-layer headers. This file is that conformance vector: a
// wrong-channel/right-header attempt MUST be refused.

// certIdentity reads the authenticated channel identity from a client cert: caller in CN,
// tenant in the first Organization.
func certIdentity(cert *x509.Certificate) (caller, tenant string) {
	caller = cert.Subject.CommonName
	if len(cert.Subject.Organization) > 0 {
		tenant = cert.Subject.Organization[0]
	}
	return
}

// makeCA returns a self-signed CA (+ its key) used to sign client certs, and a pool that
// verifies them.
func makeCA(t *testing.T) (*x509.Certificate, ed25519.PrivateKey, *x509.CertPool) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rat-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatalf("CA cert: %v", err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return ca, priv, pool
}

// issueClientCert signs a client cert under the CA encoding the channel identity
// (caller→CN, tenant→O). The attacker gets a different identity than its leaked ticket.
func issueClientCert(t *testing.T, ca *x509.Certificate, caKey ed25519.PrivateKey, serial int64, caller, tenant string) tls.Certificate {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("client key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: caller, Organization: []string{tenant}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, pub, caKey)
	if err != nil {
		t.Fatalf("client cert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

// channelAuthProducer is the CONFORMANT producer: it derives the presenting identity from
// the mTLS client certificate (the authenticated channel) and validates the ticket against
// THAT, ignoring any X-RAT-* headers. A wrong channel → 403, no bytes.
func channelAuthProducer(t *testing.T, m *Minter, clientCAs *x509.CertPool) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "no authenticated channel", http.StatusForbidden)
			return
		}
		caller, tenant := certIdentity(r.TLS.PeerCertificates[0]) // AUTHENTICATED channel identity
		ticket, _ := io.ReadAll(r.Body)
		if err := m.Validate(ticket, bulkStreamID, caller, tenant); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		_, _ = w.Write(bulkPayload)
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// mtlsFetch dials the producer presenting clientCert as the channel identity, the ticket in
// the body, and (attacker-controlled) X-RAT-* headers — which a conformant producer ignores.
func mtlsFetch(t *testing.T, endpoint string, ticket []byte, clientCert tls.Certificate, rootCAs *x509.CertPool, hdrCaller, hdrTenant string) ([]byte, int) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig:   &tls.Config{Certificates: []tls.Certificate{clientCert}, RootCAs: rootCAs},
	}}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(ticket))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("X-RAT-Caller", hdrCaller)
	req.Header.Set("X-RAT-Tenant", hdrTenant)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode
}

// TestBytesLegRequiresChannelAuth is the Q02 PU-1 conformance vector: a conformant producer
// authenticates the CHANNEL, so a leaked ticket presented over the WRONG channel is refused
// even WITH the right spoofed headers — and the rightful holder over the matching channel
// succeeds.
func TestBytesLegRequiresChannelAuth(t *testing.T) {
	ca, caKey, caPool := makeCA(t)
	legitCert := issueClientCert(t, ca, caKey, 2, bulkCaller, bulkTenant) // channel identity == ticket binding
	attackerCert := issueClientCert(t, ca, caKey, 3, "rat-evil", "tenantB")

	m := NewMinter([]byte("bulk-secret-key"))
	srv := channelAuthProducer(t, m, caPool)
	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(srv.Certificate()) // trust httptest's server cert

	// Rightful holder over the matching authenticated channel: 200 + payload.
	tk, _ := m.Mint(bulkStreamID, bulkCaller, bulkTenant, time.Minute)
	if body, code := mtlsFetch(t, srv.URL, tk, legitCert, rootCAs, bulkCaller, bulkTenant); code != http.StatusOK || !gotPayload(body) {
		t.Fatalf("rightful holder: code=%d gotPayload=%v, want 200 + payload", code, gotPayload(body))
	}

	// THE PU-1 VECTOR: a leaked, still-valid ticket presented over the ATTACKER'S
	// authenticated channel, WITH spoofed right headers — MUST be refused, because the
	// channel identity {rat-evil, tenantB} != the ticket binding {bulkCaller, tenantA} and
	// the headers are ignored. No bytes leak.
	leaked, _ := m.Mint(bulkStreamID, bulkCaller, bulkTenant, time.Minute)
	if body, code := mtlsFetch(t, srv.URL, leaked, attackerCert, rootCAs, bulkCaller, bulkTenant); code != http.StatusForbidden || gotPayload(body) {
		t.Fatalf("wrong-channel/right-header attack: code=%d gotPayload=%v, want 403 + no payload (PU-1)", code, gotPayload(body))
	}
}

// TestHeaderTrustingProducerIsFooled is the contrast that shows WHY PU-1 is mandatory: the
// header-trusting producer (the bulkleg_test.go stand-in) hands bytes to an attacker who
// presents a leaked ticket with spoofed X-RAT-* headers — the exact gap channel-auth closes.
// (A characterization of the insecure stand-in; if it is ever hardened to channel-auth this
// expectation flips to 403, which is the desired direction.)
func TestHeaderTrustingProducerIsFooled(t *testing.T) {
	m := NewMinter([]byte("bulk-secret-key"))
	srv := bulkProducer(t, m) // header-trusting stand-in

	leaked, _ := m.Mint(bulkStreamID, bulkCaller, bulkTenant, time.Minute)
	s := &commonv1.ArrowStream{Endpoint: srv.URL, Ticket: leaked}
	// Attacker holds only the leaked ticket and sets the bound identity in plain headers.
	if body, code := fetch(t, s, bulkCaller, bulkTenant); code != http.StatusOK || !gotPayload(body) {
		t.Fatalf("header-trusting producer should be fooled (the gap PU-1 closes): code=%d gotPayload=%v, want 200 + payload", code, gotPayload(body))
	}
}
