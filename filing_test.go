package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mustParseIndexJSON builds an *indexJSON fixture from a raw JSON string
// rather than a Go struct literal. indexJSON's Item field is an anonymous
// struct type, so a literal would have to repeat the full field+tag
// definition inline to match it exactly — parsing real JSON text sidesteps
// that, and it's arguably a more honest fixture anyway, since this data
// really does come from SEC's index.json over the wire.
func mustParseIndexJSON(t *testing.T, raw string) *indexJSON {
	t.Helper()
	var idx indexJSON
	if err := json.Unmarshal([]byte(raw), &idx); err != nil {
		t.Fatalf("invalid fixture JSON: %v", err)
	}
	return &idx
}

func TestFindPrimaryXML(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		formType string
		wantName string
		wantErr  bool
	}{
		{
			name: "matching Type returns that document",
			raw: `{"directory":{"item":[
				{"name":"primary_doc.xml","type":"4","size":"1024"}
			]}}`,
			formType: "4",
			wantName: "primary_doc.xml",
		},
		{
			name: "no .xml documents at all returns error",
			raw: `{"directory":{"item":[
				{"name":"primary_doc.html","type":"4","size":"2048"},
				{"name":"0001234567-26-000123.txt","type":"4","size":"4096"}
			]}}`,
			formType: "4",
			wantErr:  true,
		},
		{
			name: "xslf345 file is excluded even when its Type matches",
			raw: `{"directory":{"item":[
				{"name":"form4_xslF345_edgar.xml","type":"4","size":"512"}
			]}}`,
			formType: "4",
			wantErr:  true,
		},
		{
			name: "no Type match: fallback is the FIRST plausible xml, later ones ignored",
			raw: `{"directory":{"item":[
				{"name":"doc_a.xml","type":"other-a","size":"111"},
				{"name":"doc_b.xml","type":"other-b","size":"222"}
			]}}`,
			formType: "4",
			wantName: "doc_a.xml",
		},
		{
			name: "uppercase .XML extension is still recognized and matched",
			raw: `{"directory":{"item":[
				{"name":"PRIMARY_DOC.XML","type":"4","size":"1024"}
			]}}`,
			formType: "4",
			wantName: "PRIMARY_DOC.XML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := mustParseIndexJSON(t, tt.raw)

			name, err := findPrimaryXML(idx, tt.formType)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("got name %q, err nil; want an error", name)
				}
				if !strings.Contains(err.Error(), "no .xml document found") {
					t.Errorf("error = %q, want it to mention 'no .xml document found'", err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tt.wantName {
				t.Errorf("got name %q, want %q", name, tt.wantName)
			}
		})
	}
}

// testEdgarClient builds an edgarClient safe for use against an
// httptest.Server: 1,000,000 requests/second so the rate limiter's pacing
// is negligible in test runs. Note the ceiling this bumps into —
// newRateLimiter computes its tick interval as time.Second / requestsPerSecond,
// and an EXTREME requestsPerSecond value can round that interval down to
// 0, which panics inside time.NewTicker. 1e6 gives a 1-microsecond
// interval: comfortably fast, comfortably away from that edge.
//
// BaseURL is a placeholder here — fetchIndexJSON takes a full URL as a
// parameter and never consults client.baseURL, so it's irrelevant for
// these tests. It matters once we test pollOnce, which DOES resolve URLs
// through client.baseURL via dailyIndexURL/resolveFilingXML.
func testEdgarClient() *edgarClient {
	return newEdgarClient(EdgarClientConfig{
		UserAgent:         "edgar-fetcher-test/0.1",
		RequestsPerSecond: 1_000_000,
		BaseURL:           "http://unused.invalid",
	})
}

func TestFetchIndexJSON(t *testing.T) {
	client := testEdgarClient()

	tests := []struct {
		name              string
		handler           http.HandlerFunc
		wantErrContains   string // empty means "expect no error"
		wantFirstItemName string
	}{
		{
			name: "200 with valid JSON returns the decoded index",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"directory":{"item":[{"name":"primary_doc.xml","type":"4","size":"1024"}]}}`))
			},
			wantFirstItemName: "primary_doc.xml",
		},
		{
			name: "non-200 status returns an error mentioning the status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErrContains: "status 404",
		},
		{
			name: "malformed JSON body returns a decode error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{not valid json`))
			},
			wantErrContains: "decoding index.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A REAL local HTTP server, not a mock — fetchIndexJSON has no
			// idea this isn't sec.gov. That's only possible because
			// fetchIndexJSON takes url as a plain parameter rather than
			// hardcoding a host internally.
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			idx, err := fetchIndexJSON(context.Background(), client, server.URL)

			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("got nil error, want one containing %q", tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(idx.Directory.Item) == 0 || idx.Directory.Item[0].Name != tt.wantFirstItemName {
				t.Errorf("got items %+v, want first item name %q", idx.Directory.Item, tt.wantFirstItemName)
			}
		})
	}
}
