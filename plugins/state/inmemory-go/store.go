// store.go — the in-memory key/value state with a global monotonic revision,
// compare-and-set, and an append-only change log for Watch.
package main

import (
	"sort"
	"strings"
	"sync"
)

type entry struct {
	value    []byte
	revision int64
}

// changeEvent is one append-only log record (all PUTs; this reference has no
// Delete RPC). The log preserves revision order, which is what Watch replays.
type changeEvent struct {
	key      string
	value    []byte
	revision int64
}

type store struct {
	mu   sync.Mutex
	data map[string]entry
	log  []changeEvent
	rev  int64 // global monotonic revision
}

func newStore() *store { return &store{data: map[string]entry{}} }

func (s *store) get(key string) (found bool, value []byte, revision int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok {
		return false, nil, 0
	}
	return true, e.value, e.revision
}

// put applies compare-and-set. ifRev > 0 requires the current revision to match;
// 0 is unconditional. Returns (committed, revision): on commit, revision is the new
// rev; on conflict, revision is the current (conflicting) rev and no write happens.
// An in-memory backend always knows the outcome, so UNKNOWN never arises.
func (s *store) put(key string, value []byte, ifRev int64) (committed bool, revision int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.data[key] // zero {nil,0} if absent
	if ifRev > 0 && cur.revision != ifRev {
		return false, cur.revision
	}
	s.rev++
	s.data[key] = entry{value: value, revision: s.rev}
	s.log = append(s.log, changeEvent{key: key, value: value, revision: s.rev})
	return true, s.rev
}

func (s *store) list(prefix string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data))
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out) // deterministic for golden vectors
	return out
}

// watchBacklog returns change-log events with revision >= fromRev whose key has the
// prefix, in revision order. fromRev == 0 means "from now" (no backlog) per the
// contract; the conformance vectors use fromRev=1 to replay from the beginning.
func (s *store) watchBacklog(prefix string, fromRev int64) []changeEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fromRev == 0 {
		return nil
	}
	var out []changeEvent
	for _, e := range s.log {
		if e.revision >= fromRev && strings.HasPrefix(e.key, prefix) {
			out = append(out, e)
		}
	}
	return out
}
