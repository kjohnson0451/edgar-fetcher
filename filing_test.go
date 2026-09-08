package main

import (
	"encoding/json"
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
