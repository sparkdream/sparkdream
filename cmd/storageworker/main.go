// Command storageworker is the reference x/storage operator: it makes the
// permanent copies members request (docs/x-storage-spec.md).
//
// It follows pending requests on the chain, fetches each file from its
// sources, checks it against the requested hash, the declared size and type
// and the storage file profile, waits for a clean verdict from the content
// scanners x/storage has approved, claims the request, posts the file and
// the signed verdicts to Arweave from its own wallet, and completes the
// request once both transactions are confirmed and a gateway serves the
// right bytes. A file that is not what was declared is declined.
//
// Before running it: register the operator account in x/service under the
// storage-arweave service type, have its controller approve it for the
// backend (MsgApproveOperator), fund an Arweave wallet, and, to keep the
// operator's own key off this machine, grant a session key covering
// MsgClaimRequest, MsgExtendClaim, MsgCompleteRequest and MsgDeclineRequest.
//
// Environment:
//
//	STW_LCD               chain REST endpoint (default http://127.0.0.1:1317)
//	STW_CHAIN_ID          chain id (default sparkdream)
//	STW_PREFIX            bech32 account prefix (default sprkdrm)
//	STW_DENOM             fee denom (default: the chain's bond denom)
//	STW_FEE / STW_GAS     fee and gas per transaction (defaults 7500 and
//	                      300000: the networks' minimum gas price of 0.025, so
//	                      change both together)
//	STW_SESSION_KEY_FILE  file holding the session key's mnemonic, with
//	STW_GRANTER           the operator address it acts for; or
//	STW_MNEMONIC          the operator's own mnemonic (not recommended)
//	STW_BACKEND           x/storage backend to serve (default arweave)
//	STW_ARWEAVE_WALLET    Arweave wallet file (JWK). Keep little in it: the
//	                      council's recurring spend is the budget, not this key
//	STW_ARWEAVE_GATEWAY   Arweave gateway (default https://arweave.net)
//	STW_MIN_CONFIRMATIONS confirmations required before completing (default 10)
//	STW_STATE_FILE        the worker's notes of what it posted (default
//	                      ./storageworker-state.json). Keep it: it is what
//	                      stops a restart from paying for an upload twice
//	STW_POLL              time between passes (default 60s)
//	STW_MAX_PER_PASS      most requests claimed in one pass (default 5)
//	STW_DECLINE_UNREACHABLE  "true" to decline requests whose sources serve
//	                      nothing (default: leave them to expire and refund)
//	STW_PROXY             proxy for fetching sources and feeds
//	STW_ALLOW_PRIVATE     "true" lets fetches reach private addresses (tests only)
//
// `storageworker wallet <file>` prints a wallet's address and its balance at
// the gateway, and creates the wallet file if it does not exist.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/arweave"
	"sparkdream/tools/contentscan"
)

// Config is the worker's settings.
type Config struct {
	LCD, ChainID, Prefix, Denom string
	Fee                         int64
	Gas                         uint64
	Mnemonic                    string
	SessionKeyFile, Granter     string

	Backend          string
	ArweaveWallet    string
	ArweaveGateway   string
	MinConfirmations int64
	StateFile        string
	Poll             time.Duration
	MaxPerPass       int
	// ResendAfter is how long a posted transaction the gateway does not know
	// is left alone before it is sent again.
	ResendAfter time.Duration
	// DeclineUnreachable: decline requests whose sources serve nothing,
	// UnreachableGraceBlocks after the chain first allows it.
	DeclineUnreachable     bool
	UnreachableGraceBlocks int64
	// MaxFeedBytes bounds one file of a scanner's feed.
	MaxFeedBytes   int64
	Proxy          string
	AllowPrivate   bool
	ConfirmTimeout time.Duration
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func loadConfig() (Config, error) {
	cfg := Config{
		LCD:                    env("STW_LCD", "http://127.0.0.1:1317"),
		ChainID:                env("STW_CHAIN_ID", "sparkdream"),
		Prefix:                 env("STW_PREFIX", "sprkdrm"),
		Denom:                  os.Getenv("STW_DENOM"),
		Fee:                    envInt("STW_FEE", 7500),
		Gas:                    uint64(envInt("STW_GAS", 300_000)), //nolint:gosec // envInt refuses negatives
		Mnemonic:               os.Getenv("STW_MNEMONIC"),
		SessionKeyFile:         os.Getenv("STW_SESSION_KEY_FILE"),
		Granter:                os.Getenv("STW_GRANTER"),
		Backend:                env("STW_BACKEND", "arweave"),
		ArweaveWallet:          os.Getenv("STW_ARWEAVE_WALLET"),
		ArweaveGateway:         env("STW_ARWEAVE_GATEWAY", "https://arweave.net"),
		MinConfirmations:       envInt("STW_MIN_CONFIRMATIONS", 10),
		StateFile:              env("STW_STATE_FILE", "storageworker-state.json"),
		Poll:                   envDuration("STW_POLL", 60*time.Second),
		MaxPerPass:             int(envInt("STW_MAX_PER_PASS", 5)),
		ResendAfter:            envDuration("STW_RESEND_AFTER", 20*time.Minute),
		DeclineUnreachable:     env("STW_DECLINE_UNREACHABLE", "") == "true",
		UnreachableGraceBlocks: envInt("STW_UNREACHABLE_GRACE_BLOCKS", 0),
		MaxFeedBytes:           envInt("STW_MAX_FEED_BYTES", 64<<20),
		Proxy:                  os.Getenv("STW_PROXY"),
		AllowPrivate:           env("STW_ALLOW_PRIVATE", "") == "true",
		ConfirmTimeout:         envDuration("STW_CONFIRM_TIMEOUT", 45*time.Second),
	}
	if cfg.ArweaveWallet == "" {
		return cfg, errors.New("STW_ARWEAVE_WALLET is required")
	}
	session := cfg.SessionKeyFile != "" || cfg.Granter != ""
	switch {
	case session && cfg.Mnemonic != "":
		return cfg, errors.New("set STW_MNEMONIC or STW_SESSION_KEY_FILE + STW_GRANTER, not both")
	case session && (cfg.SessionKeyFile == "" || cfg.Granter == ""):
		return cfg, errors.New("STW_SESSION_KEY_FILE and STW_GRANTER must be set together")
	case !session && cfg.Mnemonic == "":
		return cfg, errors.New("missing signing key: set STW_SESSION_KEY_FILE + STW_GRANTER (session key) or STW_MNEMONIC")
	}
	if cfg.MaxPerPass < 1 {
		cfg.MaxPerPass = 1
	}
	return cfg, nil
}

// walletCmd prints a wallet's address and balance, creating the wallet
// first when the file does not exist.
func walletCmd(path, gatewayURL string) error {
	w, err := arweave.LoadWallet(path)
	if errors.Is(err, os.ErrNotExist) {
		if w, err = arweave.GenerateWallet(4096); err != nil {
			return err
		}
		raw, jerr := w.JWK()
		if jerr != nil {
			return jerr
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return err
		}
		fmt.Printf("created %s\n", path)
	} else if err != nil {
		return err
	}
	fmt.Printf("address: %s\n", w.Address())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bal, err := arweave.NewClient(gatewayURL).Balance(ctx, w.Address())
	if err != nil {
		fmt.Printf("balance: unknown (%v)\n", err)
		return nil
	}
	fmt.Printf("balance: %s winston at %s\n", bal, gatewayURL)
	return nil
}

func newWorker(cfg Config) (*Worker, error) {
	wallet, err := arweave.LoadWallet(cfg.ArweaveWallet)
	if err != nil {
		return nil, fmt.Errorf("arweave wallet: %w", err)
	}
	chain, err := sdaptx.New(sdaptx.Config{
		LCD: cfg.LCD, ChainID: cfg.ChainID, Bech32Prefix: cfg.Prefix, Denom: cfg.Denom,
		FeeAmount: cfg.Fee, GasLimit: cfg.Gas, Mnemonic: cfg.Mnemonic,
		SessionKeyFile: cfg.SessionKeyFile, Granter: cfg.Granter,
	})
	if err != nil {
		return nil, fmt.Errorf("chain client: %w", err)
	}
	limits := contentscan.FetchLimits{MaxBytes: cfg.MaxFeedBytes, Timeout: 60 * time.Second, AllowPrivate: cfg.AllowPrivate}
	if cfg.Proxy != "" {
		if limits.Proxy, err = url.Parse(cfg.Proxy); err != nil {
			return nil, fmt.Errorf("STW_PROXY: %w", err)
		}
	}
	client := contentscan.NewFetchClient(limits)
	w := &Worker{
		cfg:      cfg,
		chain:    chain,
		operator: chain.Address(),
		gateway:  arweave.NewClient(cfg.ArweaveGateway),
		wallet:   wallet,
		now:      time.Now,
		// Sources and feeds are attacker-chosen addresses: fetched through
		// the scanner worker's guard (no private destinations, size and
		// time limits) and never written to disk.
		fetch: func(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
			l := limits
			l.MaxBytes = maxBytes
			return contentscan.Fetch(ctx, client, rawURL, l)
		},
		send: func(ctx context.Context, msg sdk.Msg) error {
			return broadcast(ctx, chain, msg, cfg.ConfirmTimeout)
		},
	}
	if err := w.loadState(); err != nil {
		return nil, err
	}
	return w, nil
}

// broadcast signs and sends one message and waits for its result in a
// block: a message the chain refuses when it runs is an error here, not a
// success.
func broadcast(ctx context.Context, chain *sdaptx.Client, msg sdk.Msg, confirm time.Duration) error {
	for attempt := 0; attempt < 3; attempt++ {
		res, err := chain.SignAndBroadcast(ctx, msg)
		if err != nil {
			return err
		}
		if !res.OK() && res.SessionUnusable() {
			return fmt.Errorf("%w: %s", sdaptx.ErrNoSessionKey, res.RawLog)
		}
		if res.OK() {
			done, err := chain.Confirm(ctx, res.TxHash, confirm)
			if err != nil {
				return err
			}
			if !done.OK() {
				return fmt.Errorf("tx %s failed with code %d: %s", res.TxHash, done.Code, done.RawLog)
			}
			return nil
		}
		if !sdaptx.Retryable(res.Code) {
			return fmt.Errorf("tx rejected with code %d: %s", res.Code, res.RawLog)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	return errors.New("broadcast kept failing")
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) >= 2 && os.Args[1] == "wallet" {
		if len(os.Args) != 3 {
			log.Fatal("usage: storageworker wallet <file>")
		}
		if err := walletCmd(os.Args[2], env("STW_ARWEAVE_GATEWAY", "https://arweave.net")); err != nil {
			log.Fatalf("storageworker: %v", err)
		}
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("storageworker: %v", err)
	}
	w, err := newWorker(cfg)
	if err != nil {
		log.Fatalf("storageworker: %v", err)
	}
	log.Printf("storageworker: operator %s, backend %s, Arweave wallet %s via %s",
		w.operator, cfg.Backend, w.wallet.Address(), cfg.ArweaveGateway)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		if err := w.pass(ctx); err != nil && ctx.Err() == nil {
			log.Printf("storageworker: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.Poll):
		}
	}
}
