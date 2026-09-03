package main

import (
	"context"
	"log/slog"
	"os"
	"time"
)

func main() {
	// JSON output so a log aggregator sitting in front of the cluster
	// (or just `kubectl logs` piped through jq) can filter/query on fields
	// like accession number or CIK instead of grepping formatted strings.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// EDGAR_USER_AGENT must identify you per SEC's documented requirement,
	// e.g. "EdgarPipeline/0.1 (your-email@example.com)". There's no sane
	// default for this — it's not optional, so we fail fast rather than
	// silently sending a generic User-Agent that risks getting blocked.
	userAgent := requireEnv("EDGAR_USER_AGENT")
	parserURL := getEnv("PARSER_URL", "http://edgar-parser-svc:8000/api/v1/filings")
	pollInterval := getEnvDuration("POLL_INTERVAL", 15*time.Minute)

	client := newEdgarClient(userAgent, 4.0) // 4 req/sec — comfortably under SEC's 10 req/sec ceiling
	publisher := NewHTTPPublisher(parserURL)

	// In-memory only — resets on pod restart. That's a known gap, not an
	// oversight: durable dedupe state needs a StatefulSet/database, which
	// doesn't exist yet. Until then, a restart may re-publish a day's
	// filings once; edgar-parser should treat re-parses as idempotent
	// rather than assume fetcher guarantees exactly-once delivery.
	seen := make(map[string]struct{})

	ctx := context.Background()

	slog.Info("edgar-fetcher starting",
		"parser_url", parserURL,
		"poll_interval", pollInterval.String(),
	)

	for {
		if err := pollOnce(ctx, client, publisher, seen); err != nil {
			slog.Error("poll cycle failed", "error", err)
		}
		time.Sleep(pollInterval)
	}
}

func pollOnce(ctx context.Context, client *edgarClient, publisher FilingPublisher, seen map[string]struct{}) error {
	today := time.Now().UTC()

	refs, err := fetchDailyIndex(ctx, client, today, "4")
	if err != nil {
		return err
	}

	slog.Info("daily index fetched",
		"filing_count", len(refs),
		"date", today.Format("2006-01-02"),
	)

	for _, ref := range refs {
		acc := ref.AccessionNumber()
		if _, alreadySeen := seen[acc]; alreadySeen {
			continue
		}

		rawXML, err := resolveFilingXML(ctx, client, ref)
		if err != nil {
			// Log and continue rather than aborting the whole cycle — one bad
			// filing shouldn't block discovery of the rest of the day's filings.
			slog.Warn("skipping filing",
				"accession_number", acc,
				"company", ref.CompanyName,
				"error", err,
			)
			continue
		}

		envelope := FilingEnvelope{
			CIK:             ref.CIK,
			AccessionNumber: acc,
			FormType:        ref.FormType,
			FiledAt:         ref.DateFiled,
			RawXML:          rawXML,
			FetchedAt:       time.Now().UTC(),
		}

		if err := publisher.Publish(ctx, envelope); err != nil {
			slog.Error("publish failed",
				"accession_number", acc,
				"company", ref.CompanyName,
				"error", err,
			)
			continue // don't mark seen — worth retrying next cycle
		}

		seen[acc] = struct{}{}
		slog.Info("published filing",
			"accession_number", acc,
			"company", ref.CompanyName,
		)
	}

	return nil
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		// slog has no Fatal — it only writes the record, it never exits the
		// process. So an Error log + explicit os.Exit(1) is the idiomatic
		// stand-in for what log.Fatalf did in one call.
		slog.Error("required environment variable is not set", "key", key)
		os.Exit(1)
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		// Warn, not Error: we're recovering with a sane fallback, not
		// failing the request. Reserve Error for things that actually
		// degraded an outcome (a skipped filing, a failed publish).
		slog.Warn("invalid duration, using default",
			"key", key,
			"value", v,
			"default", fallback.String(),
			"error", err,
		)
		return fallback
	}
	return d
}
