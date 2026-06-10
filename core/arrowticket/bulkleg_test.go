package arrowticket

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonv1 "github.com/squat-collective/rat-v3/gen/rat/common/v1"
)

// D2 — the ticket is the ONLY gate on the bulk leg. The Arrow bytes leg bypasses the
// core, so unlike the control plane (gateway/C5) there is no mediator: a consumer
// dials the producer's endpoint DIRECTLY and the ArrowStream.ticket is the sole
// authorization. This wires the Minter (proven at the unit level in arrowticket_test.go)
// into a REAL out-of-band transfer and proves the gate end-to-end: bytes move only on a
// valid ticket; replay / cross-binding / expiry / tamper are refused at the boundary.

const (
	bulkStreamID = "stream-orders-1"
	bulkCaller   = "rat-strategy"
	bulkTenant   = "tenantA"
)

// bulkPayload stands in for the Arrow IPC record-batch bytes the real leg would carry.
var bulkPayload = []byte("arrow-ipc-record-batch::orders::v1")

// bulkProducer is a Flight-shaped (DoGet) stand-in: a real HTTP endpoint that streams
// the payload ONLY when the presented ticket validates against the PRESENTING identity
// (caller/tenant headers — the spike's stand-in for the authenticated Flight channel;
// C2 tightens the source of that identity at GA) and this endpoint's stream. A
// validation failure serves a 403 and NO bytes — the gate holds.
func bulkProducer(t *testing.T, m *Minter) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticket, _ := io.ReadAll(r.Body)
		if err := m.Validate(ticket, bulkStreamID, r.Header.Get("X-RAT-Caller"), r.Header.Get("X-RAT-Tenant")); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		_, _ = w.Write(bulkPayload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// bulkStream mints a ticket bound to {bulkStreamID, bulkCaller, bulkTenant} and packs
// it into the frozen ArrowStream the consumer reads endpoint+ticket from.
func bulkStream(t *testing.T, m *Minter, endpoint string, ttl time.Duration) *commonv1.ArrowStream {
	t.Helper()
	tk, err := m.Mint(bulkStreamID, bulkCaller, bulkTenant, ttl)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	return &commonv1.ArrowStream{
		Endpoint:  endpoint,
		Ticket:    tk,
		Transport: commonv1.ArrowTransport_ARROW_TRANSPORT_FLIGHT,
		Role:      commonv1.ArrowStreamRole_ARROW_STREAM_ROLE_PRODUCER_HOSTED,
	}
}

// bulkClient is a dedicated client with keep-alives DISABLED: each fetch is a fresh
// connection, so the test can't flake on HTTP connection reuse (and never touches the
// global http.DefaultClient). Correctness over throughput — this is a test harness.
var bulkClient = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

// fetch dials the ArrowStream endpoint, presenting the ticket + the consumer's identity
// (authenticated, in production). Returns the body + HTTP status.
func fetch(t *testing.T, s *commonv1.ArrowStream, caller, tenant string) ([]byte, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.GetEndpoint(), bytes.NewReader(s.GetTicket()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-RAT-Caller", caller)
	req.Header.Set("X-RAT-Tenant", tenant)
	resp, err := bulkClient.Do(req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode
}

func gotPayload(body []byte) bool { return bytes.Equal(body, bulkPayload) }

// TestBulkLegTicketIsTheOnlyGate drives the rejection vectors through a REAL transfer.
func TestBulkLegTicketIsTheOnlyGate(t *testing.T) {
	m := NewMinter([]byte("bulk-secret-key"))
	srv := bulkProducer(t, m)

	// Happy path: the rightful holder receives exactly the payload bytes.
	s := bulkStream(t, m, srv.URL, time.Minute)
	if body, code := fetch(t, s, bulkCaller, bulkTenant); code != http.StatusOK || !gotPayload(body) {
		t.Fatalf("happy path: code=%d gotPayload=%v, want 200 + payload", code, gotPayload(body))
	}
	// Replay (single-use): the same ticket a second time is refused — no bytes leak.
	if body, code := fetch(t, s, bulkCaller, bulkTenant); code != http.StatusForbidden || gotPayload(body) {
		t.Errorf("replay: code=%d gotPayload=%v, want 403 + no payload", code, gotPayload(body))
	}

	// Cross-binding: a leaked ticket presented from a DIFFERENT tenant's (authenticated)
	// connection is refused — and binding is checked before single-use, so the ticket is
	// NOT consumed and the rightful holder still succeeds afterward.
	leaked := bulkStream(t, m, srv.URL, time.Minute)
	if body, code := fetch(t, leaked, bulkCaller, "tenantB"); code != http.StatusForbidden || gotPayload(body) {
		t.Errorf("cross-tenant: code=%d gotPayload=%v, want 403 + no payload", code, gotPayload(body))
	}
	if body, code := fetch(t, leaked, bulkCaller, bulkTenant); code != http.StatusOK || !gotPayload(body) {
		t.Errorf("rightful holder after a failed cross-tenant attempt: code=%d gotPayload=%v, want 200 + payload", code, gotPayload(body))
	}

	// Expired: a past-TTL ticket is refused (negative ttl → already expired; no clock
	// mutation, so the shared Minter stays race-free).
	expired := bulkStream(t, m, srv.URL, -time.Minute)
	if body, code := fetch(t, expired, bulkCaller, bulkTenant); code != http.StatusForbidden || gotPayload(body) {
		t.Errorf("expired: code=%d gotPayload=%v, want 403 + no payload", code, gotPayload(body))
	}

	// Tamper: a mutated ticket fails the HMAC/JSON integrity check — refused, no bytes.
	tampered := bulkStream(t, m, srv.URL, time.Minute)
	tampered.Ticket[len(tampered.Ticket)/2] ^= 0xff
	if body, code := fetch(t, tampered, bulkCaller, bulkTenant); code != http.StatusForbidden || gotPayload(body) {
		t.Errorf("tampered: code=%d gotPayload=%v, want 403 + no payload", code, gotPayload(body))
	}
}
