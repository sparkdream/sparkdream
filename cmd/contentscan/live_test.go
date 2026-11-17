package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/contentscan"
)

// countingChain counts the transactions the worker sends.
type countingChain struct {
	chainClient
	sent int
}

func (c *countingChain) SignAndBroadcast(ctx context.Context, msgs ...sdk.Msg) (sdaptx.BroadcastResult, error) {
	c.sent++
	return c.chainClient.SignAndBroadcast(ctx, msgs...)
}

// TestLiveFlag runs the scanner's x/storage flag path against a running
// chain: real queries and a real MsgFlagRemoved signed with a session key of
// an approved scanner. It is skipped unless CS_LIVE_LCD is set;
// test/storage/worker_test.sh sets it up and runs it.
//
// The chain must hold a pending scanned request, id CS_LIVE_REQUEST_ID, for
// the hash CS_LIVE_SHA256, which nobody has flagged.
func TestLiveFlag(t *testing.T) {
	lcd := os.Getenv("CS_LIVE_LCD")
	if lcd == "" {
		t.Skip("CS_LIVE_LCD is not set")
	}
	sha, id := os.Getenv("CS_LIVE_SHA256"), os.Getenv("CS_LIVE_REQUEST_ID")
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "verdict.key")
	if err := keygen(keyFile); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CS_LCD", lcd)
	t.Setenv("CS_CHAIN_ID", os.Getenv("CS_LIVE_CHAIN_ID"))
	t.Setenv("CS_DENOM", os.Getenv("CS_LIVE_DENOM"))
	t.Setenv("CS_SESSION_KEY_FILE", os.Getenv("CS_LIVE_SESSION_KEY_FILE"))
	t.Setenv("CS_GRANTER", os.Getenv("CS_LIVE_GRANTER"))
	t.Setenv("CS_MNEMONIC", "")
	t.Setenv("CS_VERDICT_KEY_FILE", keyFile)
	t.Setenv("CS_FEED_DIR", filepath.Join(dir, "feed"))
	t.Setenv("CS_STATE_FILE", filepath.Join(dir, "state.json"))
	t.Setenv("CS_IMAGE_MODEL", "")
	t.Setenv("CS_CLASSIFIER_URL", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	w, err := newWorker(cfg)
	if err != nil {
		t.Fatal(err)
	}
	chain := &countingChain{chainClient: w.chain}
	w.chain = chain
	ctx := context.Background()

	// The request is one of the subjects the worker would scan.
	found := false
	for _, s := range w.collectStorage(ctx) {
		if s.record == "storage/request/"+id && s.wantSHA256 == sha {
			found = true
		}
	}
	if !found {
		t.Fatalf("request %s for %s is not among the scanned storage requests", id, sha)
	}
	if on, err := w.flagOnChain(ctx, sha); err != nil || on {
		t.Fatalf("before the flag: on chain = %v, err = %v", on, err)
	}

	a := contentscan.Attestation{
		V: contentscan.AttestationVersion, Worker: w.operator, Verdict: contentscan.VerdictRemoved,
		Category: contentscan.CategoryIllegal, Ruleset: contentscan.Ruleset{Version: "test"},
		ScannedHeight: 1, IssuedAt: time.Now().UTC().Truncate(time.Second),
	}
	a.Subject.ChainID = cfg.ChainID
	a.Subject.Record = "storage/request/" + id
	a.Subject.SHA256 = sha
	signed, err := contentscan.Sign(a, w.key)
	if err != nil {
		t.Fatal(err)
	}
	w.flagRemoved(ctx, subject{kind: kindStorage, record: a.Subject.Record}, signed)
	if w.flags[sha] == nil {
		t.Fatal("the flag was not noted")
	}

	// Poll as the worker does. The note stays until the flag is read back
	// from the chain, and the flag is sent once.
	deadline := time.Now().Add(2 * time.Minute)
	for len(w.flags) > 0 {
		w.sendFlags(ctx)
		if time.Now().After(deadline) {
			t.Fatalf("the flag was not on chain after 2 minutes (%d sent)", chain.sent)
		}
		time.Sleep(2 * time.Second)
	}
	if chain.sent != 1 {
		t.Fatalf("sent %d flags for one hash", chain.sent)
	}
	if on, err := w.flagOnChain(ctx, sha); err != nil || !on {
		t.Fatalf("after the flag: on chain = %v, err = %v", on, err)
	}
}
