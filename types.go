// FilingEnvelope is the JSON payload edgar-fetcher POSTs to edgar-parser.
// This shape is intentionally both the wire format now AND the future
// queue-message format later — see FilingPublisher in publisher.go for the
// abstraction that makes that swap possible without touching this struct.
type FilingEnvelope struct {
