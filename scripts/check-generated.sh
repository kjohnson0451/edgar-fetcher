#!/usr/bin/env bash
set -euo pipefail

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

go-jsonschema -p main -t --capitalization CIK --capitalization XML \
  .schema/filing-envelope.schema.json > "$TMP"

if ! diff -u filing_envelope_generated.go "$TMP"; then
  echo
  echo "filing_envelope_generated.go doesn't match what generation currently produces."
  echo "If you hand-edited it, revert and run 'make generate' instead."
  echo "If the schema changed, run 'make generate' and stage the result."
  exit 1
fi
