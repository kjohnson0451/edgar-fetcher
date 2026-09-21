.PHONY: hooks fetch-schema install-tools generate

GOJSONSCHEMA_VERSION := v0.17.0

SCHEMA_TAG  := v1.0.0
SCHEMA_URL  := https://raw.githubusercontent.com/kjohnson0451/edgar-schemas/$(SCHEMA_TAG)/schemas/filing-envelope.schema.json
SCHEMA_DIR  := .schema
SCHEMA_FILE := $(SCHEMA_DIR)/filing-envelope.schema.json

hooks:
	pip install pre-commit --break-system-packages
	pre-commit install --install-hooks

fetch-schema:
	mkdir -p $(SCHEMA_DIR)
	curl -fsSL -o $(SCHEMA_FILE) $(SCHEMA_URL)

install-tools:
	go install github.com/atombender/go-jsonschema@$(GOJSONSCHEMA_VERSION)

generate: fetch-schema install-tools
	go generate ./...
