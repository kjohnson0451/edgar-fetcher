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

func newRateLimiter(requestsPerSecond float64) *rateLimiter {
	interval := time.Duration(float64(time.Second) / requestsPerSecond)
	return &rateLimiter{ticker: time.NewTicker(interval)}
}

func (r *rateLimiter) Wait() {
	<-r.ticker.C
}
