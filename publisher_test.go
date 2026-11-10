package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHTTPPublisher_Publish_SendsCorrectRequest is the genuinely new part
// compared to fetchIndexJSON's tests: those only controlled what a fake
// server SENT BACK. This asserts what Publish actually SENT — method,
// Content-Type header, and the JSON body content — by capturing the
// request inside the handler itself before responding.
func TestHTTPPublisher_Publish_SendsCorrectRequest(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	publisher := NewHTTPPublisher(server.URL)
	envelope := FilingEnvelope{
		CIK:             "1234567",
		AccessionNumber: "0001234567-26-000123",
		FormType:        "4",
		FiledAt:         "2026-07-28",
		RawXML:          "<xml>stub</xml>",
		FetchedAt:       time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	}

	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}

	var received FilingEnvelope
	if err := json.Unmarshal(gotBody, &received); err != nil {
		t.Fatalf("server received invalid JSON: %v (body: %s)", err, gotBody)
	}
	if received.CIK != envelope.CIK {
		t.Errorf("received CIK = %q, want %q", received.CIK, envelope.CIK)
	}
	if received.AccessionNumber != envelope.AccessionNumber {
		t.Errorf("received AccessionNumber = %q, want %q", received.AccessionNumber, envelope.AccessionNumber)
	}
	if received.FormType != envelope.FormType {
		t.Errorf("received FormType = %q, want %q", received.FormType, envelope.FormType)
	}
}

// TestHTTPPublisher_Publish_StatusHandling mirrors fetchIndexJSON's
// table-driven status test — same technique, applied here to Publish's
// slightly different rule: it treats ANYTHING >= 300 as an error
// (fetchIndexJSON only accepted an exact 200). Within that, only 4xx is
// ErrFilingRejected. The 400, 499, and 500 cases sit on the edges of the
// 4xx check, so an off-by-one in either bound fails one of them.
func TestHTTPPublisher_Publish_StatusHandling(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		body            string // what the fake parser writes after the status
		wantErrContains string // empty means "expect no error"
		wantRejected    bool   // whether errors.Is(err, ErrFilingRejected)
	}{
		{name: "200 OK returns no error", status: http.StatusOK},
		{name: "204 No Content is still under 300, no error", status: http.StatusNoContent},
		{name: "300 is the >= 300 boundary — treated as an error", status: http.StatusMultipleChoices, wantErrContains: "status 300"},
		{name: "400 is the lower 4xx boundary, rejected", status: http.StatusBadRequest, wantErrContains: "status 400", wantRejected: true},
		{name: "422 is rejected and carries the parser's reason", status: http.StatusUnprocessableEntity, body: "unsupported form type", wantErrContains: "unsupported form type", wantRejected: true},
		{name: "499 is the upper 4xx boundary, rejected", status: 499, wantErrContains: "status 499", wantRejected: true},
		{name: "500 returns an error mentioning the status, not rejected", status: http.StatusInternalServerError, wantErrContains: "status 500"},
	}

	envelope := FilingEnvelope{CIK: "1234567", AccessionNumber: "0001234567-26-000123", FormType: "4"}

	// Go 1.22+ gives each loop iteration its own copy of tt, so it's safe
	// to reference tt directly inside the handler closure below — no
	// shadowing workaround needed (older Go versions required one).
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			}))
			defer server.Close()

			publisher := NewHTTPPublisher(server.URL)
			err := publisher.Publish(context.Background(), envelope)

			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("got nil error, want one containing %q", tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrContains)
				}
				if got := errors.Is(err, ErrFilingRejected); got != tt.wantRejected {
					t.Errorf("errors.Is(err, ErrFilingRejected) = %v, want %v", got, tt.wantRejected)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
