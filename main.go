package main

import (
	"context"
	"log"
	"os"
	"time"
)

func main() {
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
	// oversight: durable dedupe state is Day 44's job once the
	// StatefulSet/database exists. Until then, a restart may re-publish a
	// day's filings once; edgar-parser should treat re-parses as idempotent
	// rather than assume fetcher guarantees exactly-once delivery.
	seen := make(map[string]struct{})

	ctx := context.Background()

	log.Printf("edgar-fetcher starting: parser=%s poll_interval=%s", parserURL, pollInterval)

	for {
		if err := pollOnce(ctx, client, publisher, seen); err != nil {
			log.Printf("poll cycle failed: %v", err)
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

	log.Printf("daily index: %d Form 4 filings found for %s", len(refs), today.Format("2006-01-02"))

	for _, ref := range refs {
		acc := ref.AccessionNumber()
		if _, alreadySeen := seen[acc]; alreadySeen {
			continue
		}

		rawXML, err := resolveFilingXML(ctx, client, ref)
		if err != nil {
			// Log and continue rather than aborting the whole cycle — one bad
			// filing shouldn't block discovery of the rest of the day's filings.
			log.Printf("skipping %s (%s): %v", acc, ref.CompanyName, err)
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
			log.Printf("publish failed for %s (%s): %v", acc, ref.CompanyName, err)
			continue // don't mark seen — worth retrying next cycle
		}

		seen[acc] = struct{}{}
		log.Printf("published %s — %s", acc, ref.CompanyName)
	}

	return nil
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
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
		log.Printf("invalid %s=%q, using default %s: %v", key, v, fallback, err)
		return fallback
	}
	return d
}
