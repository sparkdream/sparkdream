package sdaptx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// The mnemonic used by every test here. BIP39 test vector, never funded.
const testMnemonic = "abandon abandon abandon abandon abandon abandon " +
	"abandon abandon abandon abandon abandon about"

// The bug this pins: New() used btcutil/bech32.Encode on raw 20-byte
// address bytes, which expects 5-bit-regrouped data and fails with
// "invalid data byte" for essentially every key — so both daemons died at
// startup. Nothing caught it because this package had no tests at all.
func TestNewDerivesTheSameAddressAsTheSDK(t *testing.T) {
	c, err := New(Config{
		LCD: "http://127.0.0.1:1", ChainID: "sparkdream-dev-1",
		Bech32Prefix: "sprkdrm", Denom: "usparz.sparkdreamdev",
		Mnemonic: testMnemonic,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Golden value: coin type 118, path m/44'/118'/0'/0/0, hrp sprkdrm.
	const want = "sprkdrm19rl4cm2hmr8afy4kldpxz3fka4jguq0apc2c3g"
	if c.Address() != want {
		t.Fatalf("Address() = %q, want %q", c.Address(), want)
	}

	// And it must agree with the SDK's own encoder for the same bytes.
	viaSDK, err := sdk.Bech32ifyAddressBytes("sprkdrm", c.addrBytes)
	if err != nil {
		t.Fatal(err)
	}
	if c.Address() != viaSDK {
		t.Fatalf("Address() = %q, SDK says %q", c.Address(), viaSDK)
	}
}

func TestNewRejectsBadMnemonic(t *testing.T) {
	if _, err := New(Config{Bech32Prefix: "sprkdrm", Mnemonic: "not a mnemonic"}); err == nil {
		t.Fatal("want error for an invalid mnemonic")
	}
}

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		LCD: srv.URL, ChainID: "sparkdream-dev-1",
		Bech32Prefix: "sprkdrm", Denom: "usparz.sparkdreamdev",
		Mnemonic: testMnemonic,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// account_number 0 is the chain's first account, not a missing one.
// Treating it as "not found" made a genesis account unable to sign.
func TestAccountAcceptsAccountNumberZero(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"account":{"account_number":"0","sequence":"7"}}`))
	})
	num, seq, err := c.account(context.Background())
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if num != 0 || seq != 7 {
		t.Fatalf("account() = (%d, %d), want (0, 7)", num, seq)
	}
}

func TestAccountRejectsEmptyResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	if _, _, err := c.account(context.Background()); err == nil {
		t.Fatal("want error when the LCD returns no account")
	}
}

// Confirm is the only way to learn a tx actually executed: SYNC-mode
// broadcast reports CheckTx, and every federation rejection that matters
// is raised in DeliverTx.
func TestConfirmSurfacesDeliverTxFailure(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tx_response":{"txhash":"ABC","height":"42",` +
			`"code":2354,"raw_log":"content with hash ... already exists","events":[]}}`))
	})
	res, err := c.Confirm(context.Background(), "ABC", 0)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if res.OK() {
		t.Fatal("a DeliverTx failure must not report OK")
	}
	if res.Code != 2354 || res.Height != 42 {
		t.Fatalf("got code=%d height=%d", res.Code, res.Height)
	}
}

func TestConfirmReadsEventAttribute(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tx_response": map[string]any{
			"txhash": "ABC", "height": "9", "code": 0, "raw_log": "",
			"events": []map[string]any{{
				"type": "federated_content_received",
				"attributes": []map[string]string{
					{"key": "peer_id", "value": "md.test"},
					{"key": "content_id", "value": "17"},
				},
			}},
		},
	})
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	res, err := c.Confirm(context.Background(), "ABC", 0)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !res.OK() {
		t.Fatal("want OK")
	}
	if got := res.AttributeValue("federated_content_received", "content_id"); got != "17" {
		t.Fatalf("content_id = %q, want \"17\"", got)
	}
	if got := res.AttributeValue("federated_content_received", "nope"); got != "" {
		t.Fatalf("absent attribute = %q, want empty", got)
	}
}

// Code 13 is ErrInsufficientFee — a fixed fee below the node's minimum,
// which a retry cannot fix. Mempool-full is 20.
func TestRetryableOnlyCoversTransientCodes(t *testing.T) {
	for code, want := range map[uint32]bool{
		CodeWrongSequence:   true,
		CodeMempoolIsFull:   true,
		CodeInsufficientFee: false,
		CodeTxUnauthorized:  false,
		0:                   false,
		2354:                false,
	} {
		if got := Retryable(code); got != want {
			t.Errorf("Retryable(%d) = %v, want %v", code, got, want)
		}
	}
}
