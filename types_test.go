package main

import "testing"

func TestFilingRef_AccessionNumber(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{
			name:     "normal path with .txt suffix",
			fileName: "edgar/data/1234567/0001234567-26-000123.txt",
			want:     "0001234567-26-000123",
		},
		{
			name:     "no directory path at all",
			fileName: "0001234567-26-000123.txt",
			want:     "0001234567-26-000123",
		},
		{
			name:     "wrong extension is left intact — TrimSuffix only strips exact match",
			fileName: "edgar/data/1234567/0001234567-26-000123.xml",
			want:     "0001234567-26-000123.xml", // .xml is NOT stripped; only ".txt" is a recognized suffix
		},
		{
			name:     "empty FileName",
			fileName: "",
			want:     ".", // path.Base("") is documented to return "."
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := FilingRef{FileName: tt.fileName}
			if got := ref.AccessionNumber(); got != tt.want {
				t.Errorf("AccessionNumber() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilingRef_AccessionNoDashes(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{
			name:     "normal accession number with dashes",
			fileName: "edgar/data/1234567/0001234567-26-000123.txt",
			want:     "000123456726000123",
		},
		{
			name:     "already no dashes present — unchanged",
			fileName: "edgar/data/1234567/000123456726000123.txt",
			want:     "000123456726000123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := FilingRef{FileName: tt.fileName}
			if got := ref.AccessionNoDashes(); got != tt.want {
				t.Errorf("AccessionNoDashes() = %q, want %q", got, tt.want)
			}
		})
	}
}
