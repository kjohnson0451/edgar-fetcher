package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// dailyIndexURL builds the URL for a given day's form-sorted index file,
// under whatever host baseURL points at. EDGAR groups these by calendar
// quarter, e.g.:
//
//	{baseURL}/Archives/edgar/daily-index/2026/QTR3/form.20260728.idx
func dailyIndexURL(baseURL string, date time.Time) string {
	quarter := (int(date.Month())-1)/3 + 1
	return fmt.Sprintf(
		"%s/Archives/edgar/daily-index/%d/QTR%d/form.%s.idx",
		baseURL, date.Year(), quarter, date.Format("20060102"),
	)
}

// fetchDailyIndex downloads and parses one day's form.idx, returning only
// rows matching formType (we only care about "4" for now, but keeping this
// parameterized means 10-Q ingestion later reuses this exact function).
//
// The .idx format is fixed-width plain text, not CSV/JSON — EDGAR has kept
// this format for backward compatibility since the 1990s. We locate column
// boundaries from the header row itself rather than hardcoding character
// offsets, since EDGAR has shifted exact widths across format eras before.
func fetchDailyIndex(ctx context.Context, client *edgarClient, date time.Time, formType string) ([]FilingRef, error) {
	url := dailyIndexURL(client.baseURL, date)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching daily index %s: %w", url, err)
	}
	defer resp.Body.Close()

	// A 404 here usually just means EDGAR hasn't published today's index yet
	// (early in the day, or a weekend/holiday with no filings). That's a
	// normal, expected condition for a polling loop — not an error worth
	// logging as a failure.
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daily index %s returned status %d", url, resp.StatusCode)
	}

	return parseFormIndex(resp.Body, formType)
}

// columnOffsets records where each column starts, discovered from the
// header line of the .idx file rather than hardcoded.
type columnOffsets struct {
	formType, companyName, cik, dateFiled, fileName int
}

func parseFormIndex(body io.Reader, formType string) ([]FilingRef, error) {
	scanner := bufio.NewScanner(body)
	// .idx files can have long lines (long company names); grow the buffer
	// past bufio's default 64KB scan limit just in case.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var cols *columnOffsets
	var refs []FilingRef

	for scanner.Scan() {
		line := scanner.Text()

		// The header row is our anchor for column positions. It looks like:
		// "Form Type   Company Name                    CIK         Date Filed  File Name"
		if cols == nil && strings.HasPrefix(line, "Form Type") {
			cols = &columnOffsets{
				formType:    strings.Index(line, "Form Type"),
				companyName: strings.Index(line, "Company Name"),
				cik:         strings.Index(line, "CIK"),
				dateFiled:   strings.Index(line, "Date Filed"),
				fileName:    strings.Index(line, "File Name"),
			}
			continue
		}

		// Skip everything before we've found the header, and skip the
		// "----" separator line that follows it.
		if cols == nil || strings.HasPrefix(line, "---") {
			continue
		}
		if len(line) < cols.fileName {
			continue // malformed/short line — skip rather than panic on a slice out of range
		}

		row := FilingRef{
			FormType:    strings.TrimSpace(line[cols.formType:cols.companyName]),
			CompanyName: strings.TrimSpace(line[cols.companyName:cols.cik]),
			CIK:         strings.TrimSpace(line[cols.cik:cols.dateFiled]),
			DateFiled:   strings.TrimSpace(line[cols.dateFiled:cols.fileName]),
			FileName:    strings.TrimSpace(line[cols.fileName:]),
		}

		if row.FormType == formType {
			refs = append(refs, row)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning form index: %w", err)
	}
	return refs, nil
}
