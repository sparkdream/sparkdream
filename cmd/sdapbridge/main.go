// Command sdapbridge is the inbound ActivityPub bridge daemon for the
// Spark Dream federation Mastodon link (phase P5; phases are defined in
// test/federation/mastodon/README.md). It is
// inbound-only: discovery through the bridge account's home timeline,
// ingestion through AS2 fetch + ap-canonical-v1 hashing, anchoring via
// MsgSubmitFederatedContent.
//
// The load-bearing shape decisions (P5.1):
//
//   - Discovery and ingestion are SEPARATE steps. Discovery polls one
//     local endpoint (GET /api/v1/timelines/home, one request per cycle
//     regardless of follow-set size). Ingestion fetches each new uri as
//     application/activity+json and canonicalizes THAT — the REST Status
//     is never hashed. The verifier has no follow relationship and no
//     app token, so the AS2 object is the only representation both
//     parties can reach.
//   - Dedupe keys on (uri, content_hash), never uri alone: an edit keeps
//     the uri and changes the hash, and re-anchoring edits is the most
//     valuable thing the link produces. The chain's ContentByHash is the
//     authority; the local state file is a cache to avoid wasted txs.
//   - Boosts (reblog != null) produce nothing; followers-only posts
//     produce nothing — visibility is asserted from the AS2 audience
//     (as#Public in to/cc), not from the REST field.
//   - Edits emit protocol_metadata.supersedes_content_id so a reader can
//     chain versions. The chain does not interpret it; it is a
//     convention for the read side.
//   - Edits are FOUND by a revisit sweep (SDA_REVISIT, default 5m), not by
//     discovery: an edited status keeps its id, so the since_id timeline
//     cursor never re-lists it. The sweep re-checks statuses anchored
//     within SDA_REVISIT_WINDOW (default 72h) in batches of 20 against the
//     bridge's own instance, and fetches AS2 only when edited_at has moved
//     past the version last hashed. Mastodon caches AS2 bodies for up to
//     3 minutes, so an edit AS2 does not serve yet is retried next sweep.
//   - Consent before content (SDA_CONSENT): by default only authors who
//     follow the bridge account are anchored ("opt-in"); "indexable"
//     accepts Mastodon's indexable setting instead. Every mode honours
//     #nobridge / #nobot in the author's profile. Checked before fetching
//     anything of theirs, outbox backfill included.
//   - One process, many peers (SDA_PEER_IDS): each source instance is its
//     own chain peer; posts are routed by host, and peers this operator
//     holds no binding on are dropped at startup.
//   - Replies to accounts the bridge does not follow never reach its home
//     timeline (Mastodon's home-feed filter). A reconcile sweep
//     (SDA_RECONCILE, default 15m) reads each followed account's recent
//     public posts directly and anchors what the timeline did not deliver.
//
// v1 signs with the raw operator key. Once the devnet reset (P3.0)
// seeds the P0.2 session allowlist, run under a session key scoped to
// MsgSubmitFederatedContent/MsgAttestOutbound instead.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/apcanon"
	"sparkdream/x/federation/types"
)

func main() {
	cfg := loadConfig()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "sdapbridge:", err)
		os.Exit(2)
	}

	chain, err := sdaptx.New(sdaptx.Config{
		LCD:          cfg.LCD,
		ChainID:      cfg.ChainID,
		Bech32Prefix: cfg.Prefix,
		Denom:        cfg.Denom,
		FeeAmount:    cfg.Fee,
		GasLimit:     cfg.Gas,
		Mnemonic:     cfg.Mnemonic,
	})
	if err != nil {
		log.Fatalf("sdapbridge: chain client: %v", err)
	}

	var signer *apcanon.HTTPSigner
	if cfg.FetchKeyID != "" {
		pem, err := os.ReadFile(cfg.FetchKeyPath)
		if err != nil {
			log.Fatalf("sdapbridge: read fetch key: %v", err)
		}
		signer, err = apcanon.LoadHTTPSigner(cfg.FetchKeyID, pem)
		if err != nil {
			log.Fatalf("sdapbridge: fetch signer: %v", err)
		}
	}

	state, err := LoadState(cfg.StatePath)
	if err != nil {
		log.Fatalf("sdapbridge: state: %v", err)
	}

	fetcher := &AS2Fetcher{Signer: signer, AllowPrivateHosts: cfg.AllowPrivateHosts}
	masto := NewMastodonClient(cfg.MastodonURL, cfg.Token)

	// Chain-side dedupe on boot: the local cache may be stale after a
	// restart or a state-file loss. Warm it from the peer's anchored
	// content before the first poll so a wiped cache cannot re-anchor
	// history (the chain would reject identical hashes anyway, but the
	// daemon should not spend txs finding that out).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bootCtx, cancelBoot := context.WithTimeout(ctx, 2*time.Minute)
	peerIDs := BoundPeers(bootCtx, chain, chain.Address(), cfg.PeerIDs)
	if len(peerIDs) == 0 {
		cancelBoot()
		log.Fatalf("sdapbridge: operator %s holds a bridge binding on none of %v", chain.Address(), cfg.PeerIDs)
	}
	for _, id := range peerIDs {
		if err := WarmCacheFromChain(bootCtx, chain, id, state); err != nil {
			log.Printf("sdapbridge: chain cache warm-up for %s failed (continuing with local state): %v", id, err)
		}
	}
	cancelBoot()
	if cfg.Consent == ConsentNone {
		log.Printf("sdapbridge: WARNING SDA_CONSENT=none: anchoring every followed author without consent. " +
			"Test instances only; never run a real peer this way.")
	}

	b := &Bridge{
		cfg:      cfg,
		masto:    masto,
		fetcher:  fetcher,
		chain:    &chainSubmitter{c: chain, operator: chain.Address(), confirmTimeout: cfg.ConfirmTimeout},
		state:    state,
		operator: chain.Address(),
		peers: newPeerSet(peerIDs, func(ctx context.Context, id string) ([]string, error) {
			return FetchContentHosts(ctx, chain, id)
		}),
		startedAt: time.Now(),
		digest: func(ctx context.Context, url string) (string, error) {
			return apcanon.FetchMediaDigest(ctx, url, apcanon.FetchOptions{
				AllowPrivateHosts: cfg.AllowPrivateHosts,
				Timeout:           cfg.MediaTimeout,
				MediaMinRate:      cfg.MediaMinRate,
			})
		},
	}

	log.Printf("sdapbridge: operator=%s peers=%v instance=%s consent=%s hash_rule=%s poll=%s",
		chain.Address(), peerIDs, cfg.MastodonURL, cfg.Consent, cfg.HashRule, cfg.PollInterval)
	if err := b.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("sdapbridge: %v", err)
	}
	log.Println("sdapbridge: stopped")
}

// chainSubmitter adapts sdaptx.Client to the Bridge's Submitter seam
// (tests run against a fake). Retries only transient CheckTx rejections,
// then waits for inclusion to learn the DeliverTx outcome.
type chainSubmitter struct {
	c              *sdaptx.Client
	operator       string
	confirmTimeout time.Duration
}

// ErrDuplicateContent reports that the chain already holds this exact
// (peer, hash) — a benign outcome, not a failure: it means some earlier
// attempt landed and the local cache lost track. The caller records the
// anchor and moves on.
var ErrDuplicateContent = errors.New("content already anchored on chain")

// errCodeDuplicateContent is x/federation's ErrDuplicateContent, read off
// the registered error rather than written as a literal: the literal was
// 2354 (ErrContentTerminal), so a post some other operator had already
// anchored failed every cycle and pinned the timeline cursor behind it.
var errCodeDuplicateContent = types.ErrDuplicateContent.ABCICode()

// ErrSupersedeRejected reports that the chain refused the supersedes link
// (x/federation ErrInvalidSupersede): typically a concurrent edit already
// superseded the record. The caller resubmits without the link.
var ErrSupersedeRejected = errors.New("chain refused the supersedes link")

// ErrPermanentRejection wraps a DeliverTx rejection that no retry of THIS
// post can fix: the peer's policy refuses it (type not allowed, identity
// blocked, content_uri host not the peer's), or the peer itself is not
// accepting content (not ACTIVE, or this operator's binding is gone).
// Retrying would pin the timeline cursor behind it, and with several peers
// on one cursor, one suspended peer would stall all the others; the caller
// skips it instead.
var ErrPermanentRejection = errors.New("content permanently rejected by the chain")

// ErrPeerThrottled wraps the chain's ErrRateLimitExceeded: the peer's
// inbound window (or the chain's per-block cap) is full. Retryable later,
// and scoped to that peer: the post is deferred and the peer held, rather
// than stalling every other peer on the shared discovery cursor.
var ErrPeerThrottled = errors.New("peer inbound rate limit reached")

// classifyDeliverTx maps x/federation rejection codes onto the errors the
// ingest path acts on. Codes are read off the registered errors, never
// written as literals.
func classifyDeliverTx(res sdaptx.TxResult) error {
	switch res.Code {
	case errCodeDuplicateContent:
		return ErrDuplicateContent
	case types.ErrRateLimitExceeded.ABCICode():
		return fmt.Errorf("%w: %s", ErrPeerThrottled, res.RawLog)
	case types.ErrInvalidSupersede.ABCICode():
		return fmt.Errorf("%w: %s", ErrSupersedeRejected, res.RawLog)
	case types.ErrContentTypeNotAllowed.ABCICode(),
		types.ErrIdentityBlocked.ABCICode(),
		types.ErrContentHostMismatch.ABCICode(),
		types.ErrCreatorHostMismatch.ABCICode(),
		types.ErrPeerNotActive.ABCICode(),
		types.ErrBridgeNotFound.ABCICode():
		return fmt.Errorf("%w (code %d): %s", ErrPermanentRejection, res.Code, res.RawLog)
	}
	return fmt.Errorf("tx %s failed in DeliverTx with code %d: %s", res.TxHash, res.Code, res.RawLog)
}

func (s *chainSubmitter) Submit(ctx context.Context, msg *types.MsgSubmitFederatedContent) (uint64, string, error) {
	msg.Operator = s.operator
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		res, err := s.c.SignAndBroadcast(ctx, msg)
		if err != nil {
			lastErr = err
		} else if res.OK() {
			// CheckTx passing only means the tx entered the mempool.
			// Every rejection that matters here — duplicate hash, the
			// per-peer inbound rate limit, a suspended bridge — is raised
			// in DeliverTx, so without this confirmation the daemon would
			// record rejected content as anchored and never retry it.
			return s.confirm(ctx, res.TxHash)
		} else {
			lastErr = fmt.Errorf("tx rejected in CheckTx with code %d: %s", res.Code, res.RawLog)
			if !sdaptx.Retryable(res.Code) {
				return 0, res.TxHash, lastErr
			}
		}
		if !sleepCtx(ctx, time.Duration(attempt+1)*2*time.Second) {
			return 0, "", ctx.Err()
		}
	}
	return 0, "", lastErr
}

// confirm waits for inclusion and reads the assigned content_id out of the
// federated_content_received event. SYNC-mode broadcast cannot report the
// id, which is why supersedes_content_id was never stamped before.
func (s *chainSubmitter) confirm(ctx context.Context, txHash string) (uint64, string, error) {
	res, err := s.c.Confirm(ctx, txHash, s.confirmTimeout)
	if err != nil {
		return 0, txHash, fmt.Errorf("confirm %s: %w", txHash, err)
	}
	if !res.OK() {
		return 0, txHash, classifyDeliverTx(res)
	}
	contentID, _ := strconv.ParseUint(
		res.AttributeValue(types.EventTypeFederatedContentReceived, types.AttributeKeyContentID),
		10, 64)
	return contentID, txHash, nil
}

func loadConfig() Config {
	fs := flag.NewFlagSet("sdapbridge", flag.ExitOnError)
	cfg := Config{
		MastodonURL: env("MASTODON_URL", ""),
		Token:       env("MASTODON_TOKEN", ""),
		LCD:         env("SDA_LCD", "https://api-dev.sparkdream.io"),
		ChainID:     env("SDA_CHAIN_ID", "sparkdream-dev-1"),
		Prefix:      env("SDA_PREFIX", "sprkdrm"),
		Denom:       env("SDA_DENOM", "usparz.sparkdreamdev"),
		// SDA_PEER_IDS is a comma-separated list; SDA_PEER_ID (one peer) is
		// still read when it is unset.
		PeerIDs:           splitList(env("SDA_PEER_IDS", env("SDA_PEER_ID", ""))),
		Consent:           ConsentMode(env("SDA_CONSENT", string(ConsentOptIn))),
		Mnemonic:          env("SDA_MNEMONIC", ""),
		StatePath:         env("SDA_STATE", "sdapbridge-state.json"),
		PollInterval:      envDuration("SDA_POLL", 30*time.Second),
		ConfirmTimeout:    envDuration("SDA_CONFIRM_TIMEOUT", 45*time.Second),
		Fee:               envInt("SDA_FEE", 750000),
		Gas:               envUint("SDA_GAS", 300000),
		FetchKeyID:        env("SDA_FETCH_KEY_ID", ""),
		FetchKeyPath:      env("SDA_FETCH_KEY", ""),
		BodyLimit:         envInt("SDA_BODY_LIMIT", 4096),
		MetadataLimit:     envInt("SDA_METADATA_LIMIT", 8192),
		HashRule:          env("SDA_HASH_RULE", apcanon.HashRuleV2),
		MediaTimeout:      envDuration("SDA_MEDIA_TIMEOUT", apcanon.DefaultMediaTimeout),
		MediaMinRate:      envInt("SDA_MEDIA_MIN_RATE", apcanon.DefaultMediaMinRate),
		Backfill:          envInt("SDA_BACKFILL", 20),
		RevisitInterval:   envDuration("SDA_REVISIT", 5*time.Minute),
		RevisitWindow:     envDuration("SDA_REVISIT_WINDOW", 72*time.Hour),
		ReconcileInterval: envDuration("SDA_RECONCILE", 15*time.Minute),
		ReconcileLookback: envDuration("SDA_RECONCILE_LOOKBACK", 24*time.Hour),
		// Local test instances only (INSTANCE_SETUP.md); never a real peer.
		AllowPrivateHosts: envBool("SDA_ALLOW_PRIVATE_HOSTS", false),
	}
	fs.Parse(os.Args[1:])
	return cfg
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v := os.Getenv(k); def != 0 && v != "" {
		var out int64
		if _, err := fmt.Sscanf(v, "%d", &out); err == nil {
			return out
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	switch os.Getenv(k) {
	case "1", "true", "TRUE", "yes":
		return true
	case "0", "false", "FALSE", "no":
		return false
	}
	return def
}

func envUint(k string, def uint64) uint64 {
	if v := os.Getenv(k); v != "" {
		var out uint64
		if _, err := fmt.Sscanf(v, "%d", &out); err == nil {
			return out
		}
	}
	return def
}
