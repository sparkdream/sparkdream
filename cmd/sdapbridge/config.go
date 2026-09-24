package main

import (
	"fmt"
	"strings"
	"time"

	"sparkdream/tools/apcanon"
)

// minPollInterval floors the discovery cadence. Zero (or a typo'd value
// that parses to zero) would otherwise hot-loop against the instance.
const minPollInterval = 5 * time.Second

// Config carries every knob of the daemon. All fields have env-var
// defaults (see loadConfig); the two credentials that MUST be supplied
// are the Mastodon app token and the operator mnemonic.
type Config struct {
	MastodonURL string // https://fedi.example
	Token       string // read-only OAuth app token (its own rotation note in the runbook)
	LCD         string // chain LCD
	ChainID     string
	Prefix      string // bech32 account prefix
	Denom       string // fee denom
	// PeerIDs are the chain peers this process anchors for: one per source
	// instance, all bound to the same operator. Each post is routed to the
	// peer that owns its host; posts from any other instance are skipped.
	PeerIDs []string
	// Consent decides whose posts may be anchored (consent.go).
	Consent      ConsentMode
	Mnemonic     string        // bridge operator key (raw-key mode until session keys land)
	StatePath    string        // local dedupe cache
	PollInterval time.Duration // discovery poll
	// ConfirmTimeout bounds the wait for tx inclusion. SYNC-mode broadcast
	// reports CheckTx only, so the daemon has to follow up before it can
	// claim anything was anchored.
	ConfirmTimeout time.Duration
	Fee            int64
	Gas            uint64
	// FetchKeyID is the HTTP-signature key id for AS2 fetches against
	// secure-mode (AUTHORIZED_FETCH) instances.
	FetchKeyID   string
	FetchKeyPath string
	BodyLimit    int64 // truncate body at max_content_body_size
	// HashRule is the rule posts are anchored under: ap-canonical-v2 (also
	// digests every attachment's file) or ap-canonical-v1. Empty is v1.
	HashRule string
	// MediaTimeout and MediaMinRate bound each attachment download under
	// ap-canonical-v2: MediaTimeout for the response headers, extended by
	// the file's declared size at MediaMinRate bytes/second. Not part of
	// the hash rule: a download that runs out of time is retried, never
	// hashed.
	MediaTimeout time.Duration
	MediaMinRate int64
	// MetadataLimit is the chain's max_protocol_metadata_size. The bridge
	// fits protocol_metadata (attachment list included) under it, because
	// the chain truncates bytes and truncated JSON is unreadable.
	MetadataLimit int64
	Backfill      int64 // outbox items per newly-seen account
	// RevisitInterval is how often anchored statuses are re-checked for
	// edits; RevisitWindow is how long after its last anchor a status stays
	// on that list. Discovery walks the home timeline forward with since_id,
	// and an edited status keeps its id and position, so without the revisit
	// sweep nothing would ever re-anchor an edit. Zero interval disables it.
	RevisitInterval time.Duration
	RevisitWindow   time.Duration
	// ReconcileInterval is how often the bridge re-reads each followed
	// account's recent public posts directly, looking back
	// ReconcileLookback, to anchor what the home timeline did not deliver.
	// Chiefly replies: Mastodon's home feed drops a reply unless the reader
	// follows the account replied to. Zero interval disables it.
	ReconcileInterval time.Duration
	ReconcileLookback time.Duration
	// AllowPrivateHosts relaxes apcanon's SSRF guard for a local test
	// instance. Production must leave this false.
	AllowPrivateHosts bool
}

func (c *Config) Validate() error {
	var missing []string
	if c.MastodonURL == "" {
		missing = append(missing, "MASTODON_URL")
	}
	if c.Token == "" {
		missing = append(missing, "MASTODON_TOKEN")
	}
	if c.Mnemonic == "" {
		missing = append(missing, "SDA_MNEMONIC")
	}
	if len(c.PeerIDs) == 0 {
		missing = append(missing, "SDA_PEER_IDS")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	if !c.Consent.valid() {
		return fmt.Errorf("SDA_CONSENT must be opt-in, indexable or none (got %q)", c.Consent)
	}
	if c.HashRule != "" && c.HashRule != apcanon.HashRuleName && c.HashRule != apcanon.HashRuleV2 {
		return fmt.Errorf("SDA_HASH_RULE must be %s or %s (got %q)", apcanon.HashRuleV2, apcanon.HashRuleName, c.HashRule)
	}
	if (c.FetchKeyID == "") != (c.FetchKeyPath == "") {
		return fmt.Errorf("SDA_FETCH_KEY_ID and SDA_FETCH_KEY must be set together")
	}
	// A zero poll interval would spin the discovery loop as fast as the
	// instance will answer, which reads as an attack from the far side.
	if c.PollInterval < minPollInterval {
		return fmt.Errorf("SDA_POLL must be at least %s (got %s)", minPollInterval, c.PollInterval)
	}
	return nil
}
