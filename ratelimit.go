package main

import "time"

// rateLimiter is a minimal, stdlib-only pacing mechanism: it simply blocks
// the caller until the next tick fires. This is simpler than a true token
// bucket (no burst allowance), which is fine here — a fetcher polling every
// several minutes has no need to burst anyway. Worth reaching for
// golang.org/x/time/rate instead if burst behavior is ever actually needed;
// not worth the extra dependency for this.
type rateLimiter struct {
	ticker *time.Ticker
}

// newRateLimiter builds a rateLimiter that allows requestsPerSecond calls
// per second, via a ticker firing at the corresponding interval. Watch the
// extremes: too LOW a requestsPerSecond (near 0) makes the interval huge —
// fine, just slow. Too HIGH rounds the interval down toward 0, and
// time.NewTicker(0) panics — see testEdgarClient in filing_test.go for the
// value we settled on to stay clear of that edge in tests.
func newRateLimiter(requestsPerSecond float64) *rateLimiter {
	interval := time.Duration(float64(time.Second) / requestsPerSecond)
	return &rateLimiter{ticker: time.NewTicker(interval)}
}

// Wait blocks until the ticker's next tick fires — nothing more than a
// channel receive. There's no Stop() anywhere in this codebase, so the
// ticker (and its underlying goroutine) runs until the process exits; not
// a concern for edgarClient's single long-lived instance in main, but
// worth knowing if a rateLimiter is ever created somewhere shorter-lived.
func (r *rateLimiter) Wait() {
	<-r.ticker.C
}
