package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakePublisher is a minimal FilingPublisher for tests — this is the exact
// seam FilingPublisher was designed for (see publisher.go's comment): swap
// in a fake without touching pollOnce at all. failFor lets a specific
// filing's Publish call be made to fail on command.
type fakePublisher struct {
	published []FilingEnvelope
	failFor   map[string]bool // keyed by AccessionNumber
}

func (p *fakePublisher) Publish(ctx context.Context, envelope FilingEnvelope) error {
	if p.failFor[envelope.AccessionNumber] {
		return fmt.Errorf("simulated publish failure for %s", envelope.AccessionNumber)
	}
	p.published = append(p.published, envelope)
	return nil
}

// dailyIndexFixture contains six rows covering every branch pollOnce can
// take in one poll cycle. See the accompanying comment on each accession
// number below for what it's meant to exercise.
const dailyIndexFixture = `Form Type   Company Name                    CIK         Date Filed  File Name
4           Already Seen Co                 1000000     2026-09-10  edgar/data/1000000/0001000000-26-000000.txt
4           Resolve Fail Co                 1000002     2026-09-10  edgar/data/1000002/0001000002-26-000002.txt
4           Publish Fail Co                 1000003     2026-09-10  edgar/data/1000003/0001000003-26-000003.txt
4           Dup Fail Co                     1000004     2026-09-10  edgar/data/1000004/0001000004-26-000004.txt
4           Dup Success Co                  9000004     2026-09-10  edgar/data/9000004/0001000004-26-000004.txt
4           Clean Success Co                1000005     2026-09-10  edgar/data/1000005/0001000005-26-000005.txt`

const validIndexJSON = `{"directory":{"item":[{"name":"primary_doc.xml","type":"4","size":"512"}]}}`
const validDocXML = `<ownershipDocument><stub>test</stub></ownershipDocument>`

// secResponse is the canned response newFakeSECServer replies with for an
// exactly-matched request path.
type secResponse struct {
	status int
	body   string
}

// newFakeSECServer routes on request path rather than exact URL:
//   - The daily-index request's exact path embeds TODAY'S real date (see
//     dailyIndexURL) — a test can't control that, so we match on prefix,
//     not an exact string. Matching exactly would make this test pass
//     today and silently break tomorrow.
//   - index.json / document requests use fixed CIK+accession paths we
//     control completely, so those ARE matched exactly, via the paths map.
func newFakeSECServer(t *testing.T, paths map[string]secResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/Archives/edgar/daily-index/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(dailyIndexFixture))
			return
		}
		if resp, ok := paths[r.URL.Path]; ok {
			w.WriteHeader(resp.status)
			w.Write([]byte(resp.body))
			return
		}
		t.Errorf("fake server got an unexpected request: %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestPollOnce_DailyIndexFails(t *testing.T) {
	// Server 404s on everything, including the daily index itself —
	// pollOnce should return that error immediately, without ever
	// reaching the per-filing loop.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newEdgarClient(EdgarClientConfig{
		UserAgent:         "edgar-fetcher-test/0.1",
		RequestsPerSecond: 1_000_000,
		BaseURL:           server.URL,
	})
	publisher := &fakePublisher{}
	seen := make(map[string]struct{})

	err := pollOnce(context.Background(), client, publisher, seen)

	if err == nil {
		t.Fatal("got nil error, want one propagated from fetchDailyIndex")
	}
	if len(seen) != 0 {
		t.Errorf("seen = %+v, want empty — nothing should be processed", seen)
	}
	if len(publisher.published) != 0 {
		t.Errorf("published = %+v, want none", publisher.published)
	}
}

func TestPollOnce_MixedBatch(t *testing.T) {
	server := newFakeSECServer(t, map[string]secResponse{
		// RESOLVE_FAIL: index.json 404s, so resolveFilingXML fails.
		"/Archives/edgar/data/1000002/000100000226000002/index.json": {status: http.StatusNotFound},

		// PUBLISH_FAIL: resolves fine; fakePublisher is configured below
		// to fail this one specifically.
		"/Archives/edgar/data/1000003/000100000326000003/index.json":       {status: http.StatusOK, body: validIndexJSON},
		"/Archives/edgar/data/1000003/000100000326000003/primary_doc.xml": {status: http.StatusOK, body: validDocXML},

		// DUP_FAIL / DUP_SUCCESS: same accession number, different CIKs —
		// first occurrence fails at the index.json hop, second succeeds.
		"/Archives/edgar/data/1000004/000100000426000004/index.json":       {status: http.StatusNotFound},
		"/Archives/edgar/data/9000004/000100000426000004/index.json":       {status: http.StatusOK, body: validIndexJSON},
		"/Archives/edgar/data/9000004/000100000426000004/primary_doc.xml": {status: http.StatusOK, body: validDocXML},

		// CLEAN_SUCCESS: resolves and publishes with no issues.
		"/Archives/edgar/data/1000005/000100000526000005/index.json":       {status: http.StatusOK, body: validIndexJSON},
		"/Archives/edgar/data/1000005/000100000526000005/primary_doc.xml": {status: http.StatusOK, body: validDocXML},

		// Deliberately NOT registering any path for ALREADY_SEEN
		// (1000000/000100000026000000) — if pollOnce's dedup check is
		// broken and it tries to resolve this filing anyway, the fake
		// server's default branch will fail the test via t.Errorf.
	})
	defer server.Close()

	client := newEdgarClient(EdgarClientConfig{
		UserAgent:         "edgar-fetcher-test/0.1",
		RequestsPerSecond: 1_000_000,
		BaseURL:           server.URL,
	})
	publisher := &fakePublisher{
		failFor: map[string]bool{"0001000003-26-000003": true}, // PUBLISH_FAIL
	}
	seen := map[string]struct{}{
		"0001000000-26-000000": {}, // pre-seed ALREADY_SEEN as already processed
	}

	err := pollOnce(context.Background(), client, publisher, seen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSeen := map[string]struct{}{
		"0001000000-26-000000": {}, // pre-existing, untouched
		"0001000004-26-000004": {}, // DUP_SUCCESS — the second duplicate, which won
		"0001000005-26-000005": {}, // CLEAN_SUCCESS
	}
	if len(seen) != len(wantSeen) {
		t.Errorf("seen has %d entries, want %d\n  got:  %+v\n  want: %+v", len(seen), len(wantSeen), seen, wantSeen)
	}
	for acc := range wantSeen {
		if _, ok := seen[acc]; !ok {
			t.Errorf("seen is missing accession %q", acc)
		}
	}

	if len(publisher.published) != 2 {
		t.Fatalf("got %d published envelopes, want 2: %+v", len(publisher.published), publisher.published)
	}
	publishedAccessions := map[string]bool{}
	for _, env := range publisher.published {
		publishedAccessions[env.AccessionNumber] = true
	}
	for _, want := range []string{"0001000004-26-000004", "0001000005-26-000005"} {
		if !publishedAccessions[want] {
			t.Errorf("expected %q to have been published, but it wasn't: %+v", want, publisher.published)
		}
	}
	if publishedAccessions["0001000003-26-000003"] {
		t.Error("PUBLISH_FAIL's accession appears in published — it should have failed, not succeeded")
	}
}
