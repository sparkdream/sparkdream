package sdaptx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
	sessiontypes "sparkdream/x/session/types"
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
	num, seq, err := c.account(context.Background(), c.Address())
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
	if _, _, err := c.account(context.Background(), c.Address()); err == nil {
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

// A second BIP39 test vector, for the rotated session key. Never funded.
const rotatedMnemonic = "legal winner thank year wave sausage worth useful " +
	"legal winner thank yellow"

// testGranter is testMnemonic's address: the account a session acts for.
const testGranter = "sprkdrm19rl4cm2hmr8afy4kldpxz3fka4jguq0apc2c3g"

// sessionLCD answers account queries for any address and records every
// broadcast tx body and account lookup.
type sessionLCD struct {
	txs     [][]byte
	lookups []string
}

func (l *sessionLCD) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				TxBytes string `json:"tx_bytes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode broadcast: %v", err)
			}
			raw, _ := base64.StdEncoding.DecodeString(body.TxBytes)
			l.txs = append(l.txs, raw)
			_, _ = w.Write([]byte(`{"tx_response":{"txhash":"H","code":0}}`))
			return
		}
		l.lookups = append(l.lookups, strings.TrimPrefix(r.URL.Path, "/cosmos/auth/v1beta1/accounts/"))
		_, _ = w.Write([]byte(`{"account":{"account_number":"5","sequence":"1"}}`))
	}
}

func newSessionClient(t *testing.T, keyFile string, lcd *sessionLCD) *Client {
	t.Helper()
	srv := httptest.NewServer(lcd.handler(t))
	t.Cleanup(srv.Close)
	c, err := New(Config{
		LCD: srv.URL, ChainID: "sparkdream-dev-1",
		Bech32Prefix: "sprkdrm", Denom: "usparz.sparkdreamdev",
		FeeAmount: 7500, GasLimit: 300_000,
		SessionKeyFile: keyFile, Granter: testGranter,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func verifyMsg() *types.MsgVerifyContent {
	return &types.MsgVerifyContent{Creator: testGranter, ContentId: 7, ContentHash: []byte{1, 2, 3}}
}

// Session mode starts before the key arrives (the launcher delivers it
// after the container is up): the daemon acts as the granter from the
// start and every broadcast fails with ErrNoSessionKey until then.
func TestSessionModeWaitsForTheKey(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "session-key")
	lcd := &sessionLCD{}
	c := newSessionClient(t, keyFile, lcd)
	if c.Address() != testGranter {
		t.Fatalf("Address() = %q, want the granter", c.Address())
	}
	if c.SignerAddress() != "" {
		t.Fatalf("SignerAddress() = %q before any key", c.SignerAddress())
	}
	if _, err := c.SignAndBroadcast(context.Background(), verifyMsg()); !errors.Is(err, ErrNoSessionKey) {
		t.Fatalf("broadcast without a key: err = %v, want ErrNoSessionKey", err)
	}
	if err := os.WriteFile(keyFile, []byte("not a mnemonic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SignAndBroadcast(context.Background(), verifyMsg()); !errors.Is(err, ErrNoSessionKey) {
		t.Fatalf("broadcast with a bad key: err = %v, want ErrNoSessionKey", err)
	}
	if len(lcd.txs) != 0 {
		t.Fatalf("broadcast %d txs without a usable key", len(lcd.txs))
	}
}

// The tx the session key sends: one MsgExecSession for the granter,
// signed by the grantee, wrapping the message untouched, with the gas
// overhead added and the fee scaled to keep the same gas price.
func TestSessionModeWrapsInMsgExecSession(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "session-key")
	if err := os.WriteFile(keyFile, []byte(rotatedMnemonic+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lcd := &sessionLCD{}
	c := newSessionClient(t, keyFile, lcd)
	grantee := c.SignerAddress()
	if grantee == "" || grantee == testGranter {
		t.Fatalf("SignerAddress() = %q, want the session key's own address", grantee)
	}
	res, err := c.SignAndBroadcast(context.Background(), verifyMsg())
	if err != nil || !res.OK() {
		t.Fatalf("broadcast: res=%+v err=%v", res, err)
	}
	if len(lcd.lookups) != 1 || lcd.lookups[0] != grantee {
		t.Fatalf("account lookups = %v, want the grantee's sequence", lcd.lookups)
	}

	decoded, err := c.txConfig.TxDecoder()(lcd.txs[0])
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}
	msgs := decoded.GetMsgs()
	if len(msgs) != 1 {
		t.Fatalf("tx carries %d msgs, want 1", len(msgs))
	}
	exec, ok := msgs[0].(*sessiontypes.MsgExecSession)
	if !ok {
		t.Fatalf("tx msg is %T, want MsgExecSession", msgs[0])
	}
	if exec.Grantee != grantee || exec.Granter != testGranter {
		t.Fatalf("exec grantee/granter = %s/%s", exec.Grantee, exec.Granter)
	}
	if len(exec.Msgs) != 1 || exec.Msgs[0].TypeUrl != "/sparkdream.federation.v1.MsgVerifyContent" {
		t.Fatalf("inner msgs = %v", exec.Msgs)
	}
	var inner types.MsgVerifyContent
	if err := inner.Unmarshal(exec.Msgs[0].Value); err != nil {
		t.Fatal(err)
	}
	if inner.Creator != testGranter || inner.ContentId != 7 {
		t.Fatalf("inner msg = %+v", inner)
	}

	feeTx := decoded.(sdk.FeeTx)
	if feeTx.GetGas() != 300_000+SessionGasOverhead {
		t.Fatalf("gas = %d", feeTx.GetGas())
	}
	// 7500 for 300k gas is 0.025/gas; 400k gas at that price is 10000
	if got := feeTx.GetFee().AmountOf("usparz.sparkdreamdev").Int64(); got != 10_000 {
		t.Fatalf("fee = %d, want 10000", got)
	}
}

// Rotation is a file write: the next broadcast signs with the new key,
// with no restart.
func TestSessionModeRotatesOnFileChange(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "session-key")
	if err := os.WriteFile(keyFile, []byte(rotatedMnemonic), 0o600); err != nil {
		t.Fatal(err)
	}
	lcd := &sessionLCD{}
	c := newSessionClient(t, keyFile, lcd)
	first := c.SignerAddress()
	if _, err := c.SignAndBroadcast(context.Background(), verifyMsg()); err != nil {
		t.Fatal(err)
	}
	third := "letter advice cage absurd amount doctor acoustic avoid letter advice cage above"
	if err := os.WriteFile(keyFile, []byte(third), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SignAndBroadcast(context.Background(), verifyMsg()); err != nil {
		t.Fatal(err)
	}
	second := c.SignerAddress()
	if second == first || second == "" {
		t.Fatalf("signer after rotation = %q (was %q)", second, first)
	}
	if lcd.lookups[1] != second {
		t.Fatalf("second tx looked up %q, want the rotated key %q", lcd.lookups[1], second)
	}
	// a removed file stops signing rather than keep a revoked key alive
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SignAndBroadcast(context.Background(), verifyMsg()); !errors.Is(err, ErrNoSessionKey) {
		t.Fatalf("after removal: err = %v, want ErrNoSessionKey", err)
	}
}

func TestSessionConfigErrors(t *testing.T) {
	for name, cfg := range map[string]Config{
		"granter without key file": {Bech32Prefix: "sprkdrm", Granter: testGranter},
		"key file without granter": {Bech32Prefix: "sprkdrm", SessionKeyFile: "/nonexistent"},
		"both modes":               {Bech32Prefix: "sprkdrm", SessionKeyFile: "/nonexistent", Granter: testGranter, Mnemonic: testMnemonic},
		"bad granter":              {Bech32Prefix: "sprkdrm", SessionKeyFile: "/nonexistent", Granter: "nope"},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

// Only the session codespace's "this key cannot act" codes count; the
// same numbers in another codespace mean something else.
func TestSessionUnusable(t *testing.T) {
	for _, tc := range []struct {
		res  BroadcastResult
		want bool
	}{
		{BroadcastResult{Codespace: "session", Code: sessiontypes.ErrSessionExpired.ABCICode()}, true},
		{BroadcastResult{Codespace: "session", Code: sessiontypes.ErrSessionNotFound.ABCICode()}, true},
		{BroadcastResult{Codespace: "session", Code: sessiontypes.ErrSpendLimitExceeded.ABCICode()}, true},
		{BroadcastResult{Codespace: "session", Code: sessiontypes.ErrExecCountExceeded.ABCICode()}, true},
		{BroadcastResult{Codespace: "session", Code: sessiontypes.ErrMsgTypeNotAllowed.ABCICode()}, false},
		{BroadcastResult{Codespace: "federation", Code: sessiontypes.ErrSessionExpired.ABCICode()}, false},
		{BroadcastResult{}, false},
	} {
		if got := tc.res.SessionUnusable(); got != tc.want {
			t.Errorf("%+v: SessionUnusable() = %v, want %v", tc.res, got, tc.want)
		}
	}
}
