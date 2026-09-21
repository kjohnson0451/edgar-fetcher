package main

// FilingEnvelope's canonical definition lives in the shared edgar-schemas
// repo (filing-envelope.schema.json) — code-generated here rather than
// hand-written, so edgar-fetcher and edgar-parser can't silently drift
// apart on the wire contract between them.
//
// Generated into filing_envelope_generated.go. Do not edit that file by
// hand — regenerate it via `make generate`.
//go:generate sh -c "go-jsonschema -p main -t --capitalization CIK --capitalization XML .schema/filing-envelope.schema.json > filing_envelope_generated.go"
