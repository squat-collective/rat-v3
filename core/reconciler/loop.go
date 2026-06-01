package reconciler

import (
	"context"
	"time"

	"github.com/rat-dev/rat/core/lease"
)

// Loop is the running reconciler on one core replica: each tick it advances leader
// election and, ONLY while it holds the lease, runs a convergence pass. The tick
// carries jitter so renewals/reconciles across replicas don't march in lockstep
// (sre#4). A follower ticks too (to keep contending) but does not converge — exactly
// one replica drives the plane at a time.
type Loop struct {
	Elector    *lease.Elector
	Reconciler *Reconciler
	Tick       time.Duration
	Jitter     func(time.Duration) time.Duration // nil == none
	Clock      func() time.Time                  // nil == time.Now
}

// Run drives the loop until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) {
	clock := l.Clock
	if clock == nil {
		clock = time.Now
	}
	jitter := l.Jitter
	if jitter == nil {
		jitter = func(time.Duration) time.Duration { return 0 }
	}
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := clock()
			if l.Elector.Step(now) {
				l.Reconciler.Reconcile(ctx, now)
			}
			t.Reset(l.Tick + jitter(l.Tick))
		}
	}
}
