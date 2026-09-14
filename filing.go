package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// indexJSON mirrors the structure EDGAR publishes at
// /Archives/edgar/data/{cik}/{accessionNoDashes}/index.json — a directory
// listing of every document inside one filing submission.
//
// ASSUMPTION FLAG: built against SEC's documented index.json shape; I
// haven't been able to fetch a live sample from this sandbox to confirm
// field names exactly (network access here doesn't reach sec.gov). Worth
// printing/inspecting one real response on first run to confirm this matches.
type indexJSON struct {
	Directory struct {
		Item []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Size string `json:"size"`
		} `json:"item"`
	} `json:"directory"`
}

// resolveFilingXML takes a FilingRef — produced by the earlier daily-index
// fetch, which only tells us a filing exists — and performs the two-hop
// lookup described in our design discussion to get its actual Form 4 XML
// content: filing's index.json, then the primary document referenced
// inside it.
func resolveFilingXML(ctx context.Context, client *edgarClient, ref FilingRef) (string, error) {
	cikTrimmed := strings.TrimLeft(ref.CIK, "0")
	if cikTrimmed == "" {
		cikTrimmed = "0"
	}

	indexURL := fmt.Sprintf(
		"%s/Archives/edgar/data/%s/%s/index.json",
		client.baseURL, cikTrimmed, ref.AccessionNoDashes(),
	)

	idx, err := fetchIndexJSON(ctx, client, indexURL)
	if err != nil {
		return "", fmt.Errorf("fetching filing index %s: %w", indexURL, err)
	}

	docName, err := findPrimaryXML(idx, ref.FormType)
	if err != nil {
		return "", fmt.Errorf("locating primary document for %s: %w", ref.AccessionNumber(), err)
	}

	docURL := fmt.Sprintf(
		"%s/Archives/edgar/data/%s/%s/%s",
		client.baseURL, cikTrimmed, ref.AccessionNoDashes(), docName,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request for %s: %w", docURL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching document %s: %w", docURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("document %s returned status %d", docURL, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading document body: %w", err)
	}
	return string(raw), nil
}

func fetchIndexJSON(ctx context.Context, client *edgarClient, url string) (*indexJSON, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var idx indexJSON
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("decoding index.json: %w", err)
	}
	return &idx, nil
}

// findPrimaryXML picks the document that represents the actual Form 4 data
// out of everything in the submission (which also includes things like
// XSL stylesheets used to render the XML nicely in a browser, and
// sometimes correspondence). Heuristic: prefer a document whose declared
// `type` matches the form type, filtering to .xml files and excluding
// anything that looks like a stylesheet-wrapped rendering copy.
//
// VERIFY ON FIRST RUN: this heuristic is my best read of EDGAR's
// conventions, not something I've tested against a live filing.
func findPrimaryXML(idx *indexJSON, formType string) (string, error) {
	var fallback string

	for _, item := range idx.Directory.Item {
		name := item.Name
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".xml") {
			continue
		}
		if strings.Contains(lower, "xslf345") {
			continue // stylesheet-wrapped rendering copy, not the primary doc
		}
		if item.Type == formType {
			return name, nil // best match: SEC-assigned type matches "4"
		}
		if fallback == "" {
			fallback = name // keep the first plausible .xml as a backstop
		}
	}

	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("no .xml document found in filing")
}
