package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"sparkdream/tools/contentscan"
)

// TestLiveChain runs the real worker against a running chain: real queries,
// real signed messages through a session key, and a stand-in for Arweave. It
// is skipped unless STW_LIVE_LCD is set; test/storage/worker_test.sh sets it
// up and runs it.
//
// The chain must hold a pending scanned request for liveFile(STW_LIVE_SHADE)
// with id STW_LIVE_REQUEST_ID, and its one approved scanner must publish the
// verdict key of seed liveSeed. STW_LIVE_OPENED_AT is the request's
// opened_at.
func TestLiveChain(t *testing.T) {
	lcd := os.Getenv("STW_LIVE_LCD")
	if lcd == "" {
		t.Skip("STW_LIVE_LCD is not set")
	}
	id, err := strconv.ParseInt(os.Getenv("STW_LIVE_REQUEST_ID"), 10, 64)
	if err != nil {
		t.Fatalf("STW_LIVE_REQUEST_ID: %v", err)
	}
	r := newRig(t)
	walletPath := filepath.Join(t.TempDir(), "wallet.json")
	raw, err := rigWallet.JWK()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(walletPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STW_LCD", lcd)
	t.Setenv("STW_CHAIN_ID", os.Getenv("STW_LIVE_CHAIN_ID"))
	t.Setenv("STW_SESSION_KEY_FILE", os.Getenv("STW_LIVE_SESSION_KEY_FILE"))
	t.Setenv("STW_GRANTER", os.Getenv("STW_LIVE_GRANTER"))
	t.Setenv("STW_MNEMONIC", "")
	t.Setenv("STW_ARWEAVE_WALLET", walletPath)
	t.Setenv("STW_STATE_FILE", filepath.Join(t.TempDir(), "state.json"))
	if d := os.Getenv("STW_LIVE_DENOM"); d != "" {
		t.Setenv("STW_DENOM", d)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	w, err := newWorker(cfg)
	if err != nil {
		t.Fatal(err)
	}
	w.gateway, w.fetch = r.gateway, r.w.fetch
	ctx := context.Background()

	req, err := w.getRequest(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != statusPending || !req.Scanned || len(req.Sources) == 0 {
		t.Fatalf("request %d is not a pending scanned request: %+v", id, req)
	}
	shade, _ := strconv.Atoi(os.Getenv("STW_LIVE_SHADE"))
	data := liveFile(t, shade)
	if contentscan.SHA256Hex(data) != req.Sha256 {
		t.Fatalf("request %d is for %s, the test file is %s", id, req.Sha256, contentscan.SHA256Hex(data))
	}
	r.sources[req.Sources[0].URL] = data
	height, err := w.height(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scanners, err := w.scanners(ctx)
	if err != nil || len(scanners) != 1 {
		t.Fatalf("want one approved scanner with a published key, got %d (%v)", len(scanners), err)
	}
	r.scanner, r.chainID = scanners[0].Address, cfg.ChainID
	r.verdict(&req, contentscan.VerdictClean, height)

	// Pass until the chain shows the request completed by this operator.
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if err := w.pass(ctx); err != nil {
			t.Fatal(err)
		}
		r.gateway.mu.Lock()
		r.gateway.confirmAll(int64(cfg.MinConfirmations))
		r.gateway.mu.Unlock()
		got, err := w.getRequest(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "REQUEST_STATUS_COMPLETED" {
			if got.Operator != w.operator {
				t.Fatalf("completed by %s, not %s", got.Operator, w.operator)
			}
			break
		}
		if got.Status != statusPending && got.Status != statusClaimed {
			t.Fatalf("request %d went to %s", id, got.Status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("request %d still %s after 3 minutes", id, got.Status)
		}
		time.Sleep(2 * time.Second)
	}
	if r.gateway.submits != 2 {
		t.Fatalf("posted %d transactions for one request", r.gateway.submits)
	}
}

// liveFile is the file TestLiveChain stores; TestLiveFile prints its hash
// and size for the script that opens the request.
func liveFile(t *testing.T, shade int) []byte {
	return testPNG(t, uint8(shade), 255)
}

// TestLiveFile writes the file and the scanner's verdict key (base64) for
// the script that sets the chain up.
func TestLiveFile(t *testing.T) {
	out := os.Getenv("STW_LIVE_FILE_OUT")
	if out == "" {
		t.Skip("STW_LIVE_FILE_OUT is not set")
	}
	shade, _ := strconv.Atoi(os.Getenv("STW_LIVE_SHADE"))
	if err := os.WriteFile(out, liveFile(t, shade), 0o600); err != nil {
		t.Fatal(err)
	}
	key := newRig(t).key.Public().(ed25519.PublicKey)
	if err := os.WriteFile(out+".verdictkey", []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
}
