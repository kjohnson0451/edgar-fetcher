package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FilingPublisher is the seam we designed for: edgar-fetcher's polling logic
// only ever talks to this interface, never to net/http directly. Today the
// only implementation is HTTPPublisher. When a queue exists later, a
// QueuePublisher satisfying this same interface drops in without touching
// main.go's polling logic at all.
//
// This is a small, single-method interface defined at the point of use,
// which is the idiomatic Go pattern — interfaces here describe what the
// *caller* needs, not what a given implementation happens to offer.
type FilingPublisher interface {
	Publish(ctx context.Context, envelope FilingEnvelope) error
}

// HTTPPublisher POSTs each filing envelope to edgar-parser's HTTP API.
type HTTPPublisher struct {
	URL    string
	Client *http.Client
}

// NewHTTPPublisher constructs an HTTPPublisher targeting url — edgar-parser's
// POST /api/v1/filings endpoint in production, or an httptest.Server's URL
// in tests. Capitalized like an exported name even though this is package
// main with nothing outside importing it — a naming convention carried
// over, not a package-boundary requirement here.
func NewHTTPPublisher(url string) *HTTPPublisher {
	return &HTTPPublisher{
		URL:    url,
		Client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Publish satisfies FilingPublisher. Note this method has a pointer receiver
// (*HTTPPublisher) — idiomatic because it holds an *http.Client and there's
// no reason to copy this struct around; pointer receivers are the default
// choice unless a type is small and clearly meant to be copied (like FilingRef).
func (p *HTTPPublisher) Publish(ctx context.Context, envelope FilingEnvelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshaling envelope: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building publish request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return fmt.Errorf("publishing to parser: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("parser returned status %d", resp.StatusCode)
	}
	return nil
}
