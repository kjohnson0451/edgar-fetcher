package main

import (
	"strings"
	"testing"
)

// Fixture rows below are hand-aligned to the column offsets produced by
// this header string (computed once, not hardcoded — see parseFormIndex):
//
//	Form Type   Company Name                    CIK         Date Filed  File Name
//	0           12                              44          56          68
const standardHeader = "Form Type   Company Name                    CIK         Date Filed  File Name"

const rowFormFour = "4           Acme Corp                       1234567     2026-07-28  edgar/data/1234567/0001234567-26-000123.txt"
const rowFormTenQ = "10-Q        Beta Inc                        7654321     2026-07-28  edgar/data/7654321/0007654321-26-000456.txt"

// Two rows sharing one accession number — issuer + reporting owner, exactly
// the case AccessionNumber()'s doc comment describes. parseFormIndex must
// NOT dedupe; that happens later, in main.go's seen map.
const rowDupIssuer = "4           Gamma LLC (Issuer)              1111111     2026-07-28  edgar/data/1111111/0009999999-26-000789.txt"
const rowDupOwner = "4           Jane Doe (Owner)                2222222     2026-07-28  edgar/data/1111111/0009999999-26-000789.txt"

// Shorter than cols.fileName (68) — must be skipped, not panic.
const rowTooShort = "4    Truncated"

// wantFilingRef is the expectation shape for a single parsed row. It
// deliberately checks AccessionNumber() — a DERIVED method, not the raw
// FileName field — because AccessionNumber() is what main.go's seen map
// actually keys on, and what every downstream consumer cares about. A test
// that only checked raw FileName could pass while AccessionNumber()'s own
// parsing logic (path.Base + TrimSuffix) was silently broken.
type wantFilingRef struct {
	FormType        string
	CompanyName     string
	CIK             string
	DateFiled       string
	AccessionNumber string
}

// assertRefsMatch checks both the count AND the field-level contents of
// each row — a count-only check can't distinguish "correct rows" from
// "the right number of rows with silently wrong data in them," which is
// exactly the failure mode a column-offset bug would produce.
func assertRefsMatch(t *testing.T, got []FilingRef, want []wantFilingRef) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d\n  got:  %+v\n  want: %+v", len(got), len(want), got, want)
	}

	for i := range want {
		g, w := got[i], want[i]
		if g.FormType != w.FormType {
			t.Errorf("refs[%d].FormType = %q, want %q", i, g.FormType, w.FormType)
		}
		if g.CompanyName != w.CompanyName {
			t.Errorf("refs[%d].CompanyName = %q, want %q", i, g.CompanyName, w.CompanyName)
		}
		if g.CIK != w.CIK {
			t.Errorf("refs[%d].CIK = %q, want %q", i, g.CIK, w.CIK)
		}
		if g.DateFiled != w.DateFiled {
			t.Errorf("refs[%d].DateFiled = %q, want %q", i, g.DateFiled, w.DateFiled)
		}
		if gotAcc := g.AccessionNumber(); gotAcc != w.AccessionNumber {
			t.Errorf("refs[%d].AccessionNumber() = %q, want %q", i, gotAcc, w.AccessionNumber)
		}
	}
}

func TestParseFormIndex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		formType string
		want     []wantFilingRef // nil means "expect an empty result"
	}{
		{
			name:     "header plus matching row returns that row",
			input:    standardHeader + "\n" + rowFormFour,
			formType: "4",
			want: []wantFilingRef{
				{FormType: "4", CompanyName: "Acme Corp", CIK: "1234567", DateFiled: "2026-07-28", AccessionNumber: "0001234567-26-000123"},
			},
		},
		{
			name:     "header plus non-matching row returns nothing",
			input:    standardHeader + "\n" + rowFormTenQ,
			formType: "4",
			want:     nil,
		},
		{
			name:     "no header row at all returns nothing",
			input:    rowFormFour, // no header line preceding it
			formType: "4",
			want:     nil,
		},
		{
			name:     "short rows are skipped, well-formed rows are unaffected",
			input:    standardHeader + "\n" + rowTooShort + "\n" + rowFormFour,
			formType: "4",
			want: []wantFilingRef{
				{FormType: "4", CompanyName: "Acme Corp", CIK: "1234567", DateFiled: "2026-07-28", AccessionNumber: "0001234567-26-000123"},
			},
		},
		{
			name:     "duplicate accession number produces two rows, no dedup",
			input:    standardHeader + "\n" + rowDupIssuer + "\n" + rowDupOwner,
			formType: "4",
			want: []wantFilingRef{
				{FormType: "4", CompanyName: "Gamma LLC (Issuer)", CIK: "1111111", DateFiled: "2026-07-28", AccessionNumber: "0009999999-26-000789"},
				{FormType: "4", CompanyName: "Jane Doe (Owner)", CIK: "2222222", DateFiled: "2026-07-28", AccessionNumber: "0009999999-26-000789"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := parseFormIndex(strings.NewReader(tt.input), tt.formType)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertRefsMatch(t, refs, tt.want)
		})
	}
}

// TestParseFormIndex_ReversedColumns_Panics documents a known sharp edge.
//
// Note this is NOT "any column reordering panics" — header detection
// anchors on strings.HasPrefix(line, "Form Type"), so Form Type can only
// ever be recognized as column offset 0. A header that puts something else
// first (e.g. Company Name before Form Type) isn't recognized as a header
// at ALL — cols stays nil, every row gets silently skipped, and the
// function returns empty with no panic. (That was my first draft of this
// test, and it failed for exactly that reason.)
//
// The real vulnerability is narrower: the four NON-anchor columns (CIK,
// Company Name, Date Filed, File Name) have no ordering validation relative
// to each other. Reordering any two of THEM — here, CIK before Company
// Name — produces line[bigger:smaller] on that field's slice expression,
// which panics. That's arguably the "good" failure mode: loud and
// immediate. See TestParseFormIndex_ShiftedColumns_ProducesCorruptedData
// below for the dangerous, silent counterpart.
func TestParseFormIndex_ReversedColumns_Panics(t *testing.T) {
	const reversedHeader = "Form Type   CIK         Company Name                    Date Filed  File Name"
	input := reversedHeader + "\n" + rowFormFour

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected a panic from invalid slice bounds, but function returned normally")
		}
	}()

	parseFormIndex(strings.NewReader(input), "4")
	t.Fatal("unreachable if the expected panic occurred")
}

// TestParseFormIndex_ShiftedColumns_SilentlyDropsMatchingFilings is a
// characterization test: it pins down a specific known-wrong behavior so
// it can't silently change unnoticed, rather than asserting correctness.
//
// Scenario: EDGAR inserts a new column ("Filer Type") between Form Type and
// Company Name, shifting every later column's real position further right.
// The header reflects the new layout, but this fixture's data row is built
// using the OLD layout — simulating a header/data mismatch. The row is
// well past the length guard, so nothing panics; the code just slices the
// wrong characters into FormType ("4           Acme Corp" instead of "4").
// That corrupted value no longer equals the requested formType, so
// parseFormIndex's own filter drops the row — a genuine Form 4 silently
// disappears from the results with no error and no panic.
func TestParseFormIndex_ShiftedColumns_SilentlyDropsMatchingFilings(t *testing.T) {
	const shiftedHeader = "Form Type   Filer Type   Company Name                    CIK         Date Filed  File Name"
	// Data row still laid out per the OLD (pre-shift) offsets.
	input := shiftedHeader + "\n" + rowFormFour

	refs, err := parseFormIndex(strings.NewReader(input), "4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(refs) != 0 {
		t.Errorf("got %d refs, want 0 — a genuine Form 4 filing should have silently vanished due to the column shift, not survived: %+v", len(refs), refs)
	}
}
