// Command contentscan is the reference content-scanner worker
// (docs/content-scanning.md §5). It follows the chain over the LCD, judges
// media-labelled bodies, off-chain references, collect URIs and URLs inside
// rendered bodies, appends signed verdicts to a static feed directory, and
// checkpoints the feed's merkle root on chain with MsgSubmitCheckpoint.
//
// It serves no reader traffic and holds no media at rest: fetched bytes are
// hashed and classified in memory. Publish the feed directory with any static
// host; keep the state file private.
//
// Usage:
//
//	contentscan keygen <verdict-key-file>   create an ed25519 verdict key
//	contentscan metadata                    print operator metadata JSON for
//	                                        MsgRegisterOperator / MsgUpdateMetadata
//	contentscan                             run the worker
//
// Configuration (environment):
//
//	CS_LCD                LCD base URL (default http://localhost:1317)
//	CS_CHAIN_ID           chain id (default sparkdream)
//	CS_PREFIX             bech32 prefix (default sprkdrm)
//	CS_DENOM / CS_FEE / CS_GAS   fee denom, amount and gas for checkpoints
//	CS_MNEMONIC           signing mnemonic (direct mode), or
//	CS_SESSION_KEY_FILE + CS_GRANTER   x/session key acting for the operator
//	                      (recommended: the worker host never holds the bond key)
//	CS_VERDICT_KEY_FILE   ed25519 verdict key (contentscan keygen)
//	CS_FEED_DIR           feed directory to publish (default ./feed)
//	CS_FEED_URL           public URL of the feed (for `metadata`)
//	CS_STATE_FILE         private state file (default ./contentscan-state.json)
//	CS_TAKEDOWN_LIST      takedown hash list (takedown-hashes.json or hex lines)
//	CS_CANARY_LIST        council canary hash list (hex lines)
//	CS_CLASSIFIER_URL     model server for media (POST bytes -> {"scores":{...}})
//	CS_RULESET_VERSION    ruleset version stamped into verdicts
//	CS_MODELS             JSON object of model name -> weights hash
//	CS_THRESHOLDS         JSON object of score thresholds (sexual, minor, violence, spam)
//	CS_IPFS_GATEWAY / CS_ARWEAVE_GATEWAY / CS_FILECOIN_GATEWAY / CS_JACKAL_GATEWAY
//	                      URL templates with {id}
//	CS_URI_TTL            lifetime of plain-URI verdicts (default 720h)
//	CS_POLL               poll interval (default 30s)
//	CS_CHECKPOINT_EVERY   blocks between checkpoints (default 600; 0 disables)
//	CS_MAX_FETCH_BYTES    fetch and decompression cap (default 16 MiB)
//	CS_PROXY              egress proxy URL (VPN sidecar)
//	CS_ALLOW_PRIVATE      allow private/loopback fetch targets (devnets only)
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/contentscan"
)

// Config is the worker configuration.
type Config struct {
	LCD, ChainID, Prefix, Denom string
	Fee                         int64
	Gas                         uint64
	Mnemonic                    string
	SessionKeyFile, Granter     string

	VerdictKeyFile string
	FeedDir        string
	FeedURL        string
	StateFile      string
	TakedownList   string
	CanaryList     string
	ClassifierURL  string
	RulesetVersion string
	Models         map[string]string
	Thresholds     map[string]float64
	Gateways       map[string]string // content type -> URL template with {id}

	URITTL          time.Duration
	Poll            time.Duration
	CheckpointEvery int64
	MaxFetchBytes   int64
	Proxy           string
	AllowPrivate    bool
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
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

func loadConfig() (Config, error) {
	cfg := Config{
		LCD:            strings.TrimRight(env("CS_LCD", "http://localhost:1317"), "/"),
		ChainID:        env("CS_CHAIN_ID", "sparkdream"),
		Prefix:         env("CS_PREFIX", "sprkdrm"),
		Denom:          env("CS_DENOM", "uspark"),
		Fee:            envInt("CS_FEE", 5000),
		Gas:            uint64(envInt("CS_GAS", 200_000)),
		Mnemonic:       os.Getenv("CS_MNEMONIC"),
		SessionKeyFile: os.Getenv("CS_SESSION_KEY_FILE"),
		Granter:        os.Getenv("CS_GRANTER"),
		VerdictKeyFile: env("CS_VERDICT_KEY_FILE", "verdict.key"),
		FeedDir:        env("CS_FEED_DIR", "feed"),
		FeedURL:        os.Getenv("CS_FEED_URL"),
		StateFile:      env("CS_STATE_FILE", "contentscan-state.json"),
		TakedownList:   os.Getenv("CS_TAKEDOWN_LIST"),
		CanaryList:     os.Getenv("CS_CANARY_LIST"),
		ClassifierURL:  os.Getenv("CS_CLASSIFIER_URL"),
		RulesetVersion: env("CS_RULESET_VERSION", "dev"),
		Thresholds:     map[string]float64{"sexual": 0.8, "minor": 0.6, "violence": 0.9, "spam": 0.95},
		Gateways: map[string]string{
			"CONTENT_TYPE_IPFS":     env("CS_IPFS_GATEWAY", "https://ipfs.io/ipfs/{id}"),
			"CONTENT_TYPE_ARWEAVE":  env("CS_ARWEAVE_GATEWAY", "https://arweave.net/{id}"),
			"CONTENT_TYPE_FILECOIN": env("CS_FILECOIN_GATEWAY", "https://ipfs.io/ipfs/{id}"),
			"CONTENT_TYPE_JACKAL":   os.Getenv("CS_JACKAL_GATEWAY"),
		},
		URITTL:          envDuration("CS_URI_TTL", 720*time.Hour),
		Poll:            envDuration("CS_POLL", 30*time.Second),
		CheckpointEvery: envInt("CS_CHECKPOINT_EVERY", 600),
		MaxFetchBytes:   envInt("CS_MAX_FETCH_BYTES", 16<<20),
		Proxy:           os.Getenv("CS_PROXY"),
		AllowPrivate:    env("CS_ALLOW_PRIVATE", "") == "true",
	}
	if v := os.Getenv("CS_MODELS"); v != "" {
		if err := json.Unmarshal([]byte(v), &cfg.Models); err != nil {
			return cfg, fmt.Errorf("CS_MODELS: %w", err)
		}
	}
	if v := os.Getenv("CS_THRESHOLDS"); v != "" {
		cfg.Thresholds = map[string]float64{}
		if err := json.Unmarshal([]byte(v), &cfg.Thresholds); err != nil {
			return cfg, fmt.Errorf("CS_THRESHOLDS: %w", err)
		}
	}
	return cfg, nil
}

// gateway returns the URL template for an off-chain content type.
func (c Config) gateway(contentType string) string { return c.Gateways[contentType] }

// resolveURI maps a URI to a fetchable URL. contentAddressed is true for
// ipfs:// and ar:// URIs, whose bytes cannot change (no verdict expiry).
func (c Config) resolveURI(uri string) (target string, contentAddressed bool) {
	switch {
	case strings.HasPrefix(uri, "ipfs://"):
		return strings.ReplaceAll(c.Gateways["CONTENT_TYPE_IPFS"], "{id}", strings.TrimPrefix(uri, "ipfs://")), true
	case strings.HasPrefix(uri, "ar://"):
		return strings.ReplaceAll(c.Gateways["CONTENT_TYPE_ARWEAVE"], "{id}", strings.TrimPrefix(uri, "ar://")), true
	case strings.HasPrefix(uri, "https://"), strings.HasPrefix(uri, "http://"):
		return uri, false
	}
	return "", false
}

// loadVerdictKey reads a base64 ed25519 seed.
func loadVerdictKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("verdict key file must hold a base64 32-byte ed25519 seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func keygen(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s exists; refusing to overwrite a verdict key", path)
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(seed)+"\n"), 0o600); err != nil {
		return err
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("contentscan: %v", err)
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "keygen":
			if len(os.Args) != 3 {
				log.Fatal("usage: contentscan keygen <verdict-key-file>")
			}
			if err := keygen(os.Args[2]); err != nil {
				log.Fatalf("contentscan: %v", err)
			}
			return
		case "metadata":
			key, err := loadVerdictKey(cfg.VerdictKeyFile)
			if err != nil {
				log.Fatalf("contentscan: %v", err)
			}
			md, err := contentscan.OperatorMetadata{
				VerdictKey:     base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
				FeedURL:        cfg.FeedURL,
				RulesetVersion: cfg.RulesetVersion,
				Models:         cfg.Models,
			}.Marshal()
			if err != nil {
				log.Fatalf("contentscan: %v", err)
			}
			fmt.Println(string(md))
			return
		default:
			log.Fatalf("contentscan: unknown command %q", os.Args[1])
		}
	}

	w, err := newWorker(cfg)
	if err != nil {
		log.Fatalf("contentscan: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("contentscan: worker %s scanning %s every %s", w.operator, cfg.LCD, cfg.Poll)
	for {
		if err := w.poll(ctx); err != nil && ctx.Err() == nil {
			log.Printf("contentscan: poll: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.Poll):
		}
	}
}

func newWorker(cfg Config) (*Worker, error) {
	key, err := loadVerdictKey(cfg.VerdictKeyFile)
	if err != nil {
		return nil, fmt.Errorf("verdict key: %w", err)
	}
	chain, err := sdaptx.New(sdaptx.Config{
		LCD: cfg.LCD, ChainID: cfg.ChainID, Bech32Prefix: cfg.Prefix, Denom: cfg.Denom,
		FeeAmount: cfg.Fee, GasLimit: cfg.Gas, Mnemonic: cfg.Mnemonic,
		SessionKeyFile: cfg.SessionKeyFile, Granter: cfg.Granter,
	})
	if err != nil {
		return nil, fmt.Errorf("chain client: %w", err)
	}

	limits := contentscan.FetchLimits{MaxBytes: cfg.MaxFetchBytes, Timeout: 30 * time.Second, AllowPrivate: cfg.AllowPrivate}
	if cfg.Proxy != "" {
		if limits.Proxy, err = url.Parse(cfg.Proxy); err != nil {
			return nil, fmt.Errorf("CS_PROXY: %w", err)
		}
	}
	client := contentscan.NewFetchClient(limits)

	det := contentscan.Detector{
		Ruleset: contentscan.Ruleset{Version: cfg.RulesetVersion, Models: cfg.Models, Thresholds: cfg.Thresholds},
	}
	if cfg.TakedownList != "" {
		if det.Takedown, err = contentscan.LoadHashList(cfg.TakedownList, contentscan.CategoryIllegal); err != nil {
			return nil, fmt.Errorf("takedown list: %w", err)
		}
	}
	if cfg.CanaryList != "" {
		if det.Canary, err = contentscan.LoadHashList(cfg.CanaryList, contentscan.CategoryTest); err != nil {
			return nil, fmt.Errorf("canary list: %w", err)
		}
	}
	if cfg.ClassifierURL != "" {
		det.Classifier = contentscan.HTTPClassifier{URL: cfg.ClassifierURL}
	} else {
		log.Printf("contentscan: no CS_CLASSIFIER_URL: media will stay unchecked (hash lists still apply)")
	}

	operator := chain.Address()
	feed, err := contentscan.OpenFeed(cfg.FeedDir, operator, base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
	if err != nil {
		return nil, err
	}
	w := &Worker{
		cfg: cfg, chain: chain, feed: feed, detector: det, key: key, operator: operator,
		now:  time.Now,
		done: map[string]time.Time{},
		fetch: func(ctx context.Context, rawURL string) ([]byte, error) {
			return contentscan.Fetch(ctx, client, rawURL, limits)
		},
	}
	atts, err := contentscan.ReadSegments(cfg.FeedDir + "/segments")
	if err != nil {
		return nil, err
	}
	w.loadDone(atts)
	return w, nil
}
