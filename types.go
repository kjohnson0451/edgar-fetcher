package main

import (
	"path"
	"strings"
)

// FilingRef is a single row parsed out of EDGAR's daily form.idx file — just
// enough to know a filing exists and where to find it. It does NOT yet
// contain the filing's actual content; that's a second hop (see filing.go).
type FilingRef struct {
	FormType    string
	CompanyName string
	CIK         string // as printed in the index — may or may not be zero-padded to 10 digits
	DateFiled   string // YYYY-MM-DD
	FileName    string // relative path, e.g. "edgar/data/1234567/0001234567-26-000123.txt"
}

// AccessionNumber extracts the accession number (e.g. "0001234567-26-000123")
// from the FileName column. This is the unique ID EDGAR assigns to a
// submission, and it's what we dedupe on across daily-index polls — note
// that the *same* accession number appears multiple times in EDGAR listings
// when a filing has multiple parties (issuer + reporting owner), so dedupe
// on this, not on row count.
func (f FilingRef) AccessionNumber() string {
	base := path.Base(f.FileName)
	return strings.TrimSuffix(base, ".txt")
}

// AccessionNoDashes is the same accession number with dashes stripped — this
// is the directory-name form EDGAR uses under
// /Archives/edgar/data/{cik}/{accessionNoDashes}/.
func (f FilingRef) AccessionNoDashes() string {
	return strings.ReplaceAll(f.AccessionNumber(), "-", "")
}
