// stream.go — a trivial stand-in for the out-of-band Arrow data leg.
//
// The engine/v1 contract says query results move out-of-band as Arrow IPC,
// described by a common.v1.ArrowStream {endpoint, ticket, transport=FLIGHT, role}.
// Standing up a real Arrow Flight server is out of scope for a contract-validation
// reference; instead this plugin runs an in-process stream registry: Query/Preview
// stash result rows under a ticket and advertise a producer-hosted ArrowStream;
// the caller pulls them back by ticket. (Same approach as the format reference.)
package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	commonv1 "github.com/le-squat/rat/gen/rat/common/v1"
)

// streamRegistry holds row batches keyed by ticket. Single-use: a pull deletes.
type streamRegistry struct {
	mu      sync.Mutex
	batches map[string][]row
}

func newStreamRegistry() *streamRegistry {
	return &streamRegistry{batches: map[string][]row{}}
}

func newTicket() []byte {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return b
}

// put stashes rows and returns a producer-hosted ArrowStream descriptor: the
// holder dials in to READ (the Query/Preview-read direction).
func (r *streamRegistry) put(rows []row) *commonv1.ArrowStream {
	t := newTicket()
	r.mu.Lock()
	r.batches[hex.EncodeToString(t)] = rows
	r.mu.Unlock()
	return &commonv1.ArrowStream{
		Endpoint:  "inproc://stream",
		Ticket:    t,
		Transport: commonv1.ArrowTransport_ARROW_TRANSPORT_FLIGHT,
		Role:      commonv1.ArrowStreamRole_ARROW_STREAM_ROLE_PRODUCER_HOSTED,
	}
}

// pull retrieves (and removes) the rows for a stream's ticket. Single-use.
func (r *streamRegistry) pull(s *commonv1.ArrowStream) []row {
	if s == nil {
		return nil
	}
	key := hex.EncodeToString(s.GetTicket())
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := r.batches[key]
	delete(r.batches, key)
	return rows
}
