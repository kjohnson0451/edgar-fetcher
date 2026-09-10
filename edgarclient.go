package main

import (
	"net/http"
	"time"
)

// edgarClient wraps http.Client with two things SEC expects of automated
// tools hitting sec.gov:
//  1. A descriptive User-Agent identifying the requester — SEC's one
//     documented hard requirement for scripted access.
//  2. Self-imposed pacing, kept comfortably under the documented 10 req/sec
//     ceiling. A polling fetcher has no real need to approach that limit,
//     so we stay conservative rather than testing the edge of it.
type edgarClient struct {
	http      *http.Client
	userAgent string
	limiter   *rateLimiter
}

// EdgarClientConfig bundles newEdgarClient's parameters into a named
// struct rather than a growing positional argument list. Each field is
// named at the call site (EdgarClientConfig{UserAgent: ..., ...}), so
// adding a field later doesn't force every existing call site to be
// re-read to figure out what a new positional value means.
type EdgarClientConfig struct {
	UserAgent         string
	RequestsPerSecond float64
}

func newEdgarClient(cfg EdgarClientConfig) *edgarClient {
	return &edgarClient{
		http:      &http.Client{Timeout: 30 * time.Second},
		userAgent: cfg.UserAgent,
		limiter:   newRateLimiter(cfg.RequestsPerSecond),
	}
}

// Do wraps http.Client.Do: waits for rate-limiter clearance, sets the
// required header, then performs the request. Every EDGAR call in this
// program should go through here rather than calling http.Client directly —
// that's what makes the rate limit and User-Agent requirement structurally
// impossible to forget on a new call site.
func (c *edgarClient) Do(req *http.Request) (*http.Response, error) {
	c.limiter.Wait()
	req.Header.Set("User-Agent", c.userAgent)
	return c.http.Do(req)
}
