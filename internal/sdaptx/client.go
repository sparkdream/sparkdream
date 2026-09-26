// Package sdaptx is the thin LCD-based transaction client shared by the
// federation daemons (cmd/sdapbridge, cmd/sdapverify). It signs with a
// raw key derived from a mnemonic using the chain's standard coin type
// (118, path m/44'/118'/0'/0/0) and SIGN_MODE_DIRECT, then broadcasts
// through the LCD's /cosmos/tx/v1beta1/txs endpoint in SYNC mode.
//
// Two signing modes:
//
//   - Direct: the account's own mnemonic signs its messages. Fine for a
//     daemon run on a machine the account holder controls.
//   - Session: an x/session SESSION_KEY grantee signs, and every message
//     goes out wrapped in MsgExecSession on behalf of the granter, who
//     pays the fees out of the session's spend limit. The granter's
//     mnemonic never reaches the daemon's host: a compromised host leaks a
//     key scoped to the grant's allowed_msg_types, spend_limit, exec cap
//     and expiration, which the granter revokes with one tx. The key is
//     read from a file on every broadcast, so whoever holds the granter
//     key rotates it by writing the file, without a restart; until a key
//     is there, broadcasts fail with ErrNoSessionKey and the daemon keeps
//     polling.
package sdaptx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/go-bip39"

	"sparkdream/x/federation/types"
	sessiontypes "sparkdream/x/session/types"
)

// Config configures a Client.
type Config struct {
	LCD     string // e.g. https://api-dev.sparkdream.io (no trailing slash)
	ChainID string // e.g. sparkdream-dev-1
	// Bech32Prefix for accounts (sprkdrm). Also set on the global sdk
	// config so Msg stringly-typed address validation passes.
	Bech32Prefix string
	Denom        string // fee denom, bond-denom micro units
	FeeAmount    int64  // fee in denom micro-units
	GasLimit     uint64
	// Mnemonic of the signing account (BIP39, 12/24 words). Direct mode.
	Mnemonic string
	// SessionKeyFile holds the BIP39 mnemonic of an x/session grantee
	// (session mode). Set together with Granter, instead of Mnemonic.
	SessionKeyFile string
	// Granter is the account the session acts for: the address the
	// daemons act as (Address) and the one that pays the fees.
	Granter string
}

// CheckSigningConfig validates a daemon's signing settings: a mnemonic, or
// a session key file and granter together. It names the env variables the
// daemons read them from.
func CheckSigningConfig(mnemonic, sessionKeyFile, granter string) error {
	session := sessionKeyFile != "" || granter != ""
	switch {
	case session && mnemonic != "":
		return fmt.Errorf("set SDA_MNEMONIC or SDA_SESSION_KEY_FILE + SDA_GRANTER, not both")
	case session && (sessionKeyFile == "" || granter == ""):
		return fmt.Errorf("SDA_SESSION_KEY_FILE and SDA_GRANTER must be set together")
	case !session && mnemonic == "":
		return fmt.Errorf("missing signing key: set SDA_SESSION_KEY_FILE + SDA_GRANTER (session key) or SDA_MNEMONIC")
	}
	return nil
}

// SessionGasOverhead is the gas MsgExecSession adds on top of the wrapped
// message: the grant read and write, the allowlist checks and the nested
// dispatch. Added to GasLimit in session mode, with the fee scaled in
// proportion so the tx still meets the node's minimum gas price.
const SessionGasOverhead = 100_000

// ErrNoSessionKey reports that session mode has no usable key file yet:
// the granter has not delivered one, or it is not a valid mnemonic.
var ErrNoSessionKey = errors.New("sdaptx: no session key")

// Client signs and broadcasts through an LCD endpoint.
type Client struct {
	cfg       Config
	http      *http.Client
	txConfig  client.TxConfig
	privKey   cryptotypes.PrivKey
	addrBytes []byte
	addrStr   string
	// session mode: the mnemonic the current key was derived from, so an
	// unchanged file is not re-derived on every broadcast
	sessionMnemonic string
	// keyMu guards the key fields, which session mode swaps on rotation
	keyMu sync.Mutex
}

// New derives the key, sets the bech32 prefix, and registers the minimal
// codec surface the federation messages need. In session mode the key file
// may not exist yet; New succeeds and broadcasts wait for it.
func New(cfg Config) (*Client, error) {
	if cfg.FeeAmount == 0 {
		cfg.FeeAmount = 5000
	}
	if cfg.GasLimit == 0 {
		cfg.GasLimit = 300_000
	}
	sdk.GetConfig().SetBech32PrefixForAccount(cfg.Bech32Prefix, cfg.Bech32Prefix+"pub")

	ir := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(ir)
	types.RegisterInterfaces(ir)
	sessiontypes.RegisterInterfaces(ir)
	cdc := codec.NewProtoCodec(ir)
	c := &Client{
		cfg:      cfg,
		http:     &http.Client{Timeout: 30 * time.Second},
		txConfig: authtx.NewTxConfig(cdc, authtx.DefaultSignModes),
	}

	if cfg.SessionKeyFile != "" || cfg.Granter != "" {
		if cfg.SessionKeyFile == "" || cfg.Granter == "" {
			return nil, fmt.Errorf("sdaptx: session mode needs both a session key file and a granter")
		}
		if cfg.Mnemonic != "" {
			return nil, fmt.Errorf("sdaptx: set either a mnemonic or a session key, not both")
		}
		if _, err := sdk.AccAddressFromBech32(cfg.Granter); err != nil {
			return nil, fmt.Errorf("sdaptx: granter %q: %w", cfg.Granter, err)
		}
		// best effort: a missing file is the normal state until the
		// granter delivers one
		_ = c.loadSessionKey()
		return c, nil
	}

	if err := c.setKey(cfg.Mnemonic); err != nil {
		return nil, err
	}
	return c, nil
}

// setKey derives the signing key from a mnemonic.
func (c *Client) setKey(mnemonic string) error {
	if !bip39.IsMnemonicValid(mnemonic) {
		return fmt.Errorf("sdaptx: invalid mnemonic")
	}
	derived, err := hd.Secp256k1.Derive()(mnemonic, "", hd.CreateHDPath(118, 0, 0).String())
	if err != nil {
		return fmt.Errorf("sdaptx: derive key from mnemonic: %w", err)
	}
	priv := &secp256k1.PrivKey{Key: derived}
	addr := priv.PubKey().Address()
	// sdk.Bech32ifyAddressBytes, not btcutil/bech32.Encode: the latter
	// expects data already regrouped into 5-bit words and errors out
	// ("invalid data byte") on a raw 20-byte address for essentially every
	// key. The SDK helper does the ConvertBits(8, 5) first.
	addrStr, err := sdk.Bech32ifyAddressBytes(c.cfg.Bech32Prefix, addr.Bytes())
	if err != nil {
		return fmt.Errorf("sdaptx: bech32-encode address: %w", err)
	}
	c.privKey, c.addrBytes, c.addrStr = priv, addr.Bytes(), addrStr
	return nil
}

// loadSessionKey (re)reads the session key file, re-deriving only when its
// content changed. A missing, empty or invalid file clears the key.
func (c *Client) loadSessionKey() error {
	c.keyMu.Lock()
	defer c.keyMu.Unlock()
	raw, err := os.ReadFile(c.cfg.SessionKeyFile)
	mnemonic := strings.Join(strings.Fields(string(raw)), " ")
	if err != nil || mnemonic == "" {
		c.privKey, c.addrBytes, c.addrStr, c.sessionMnemonic = nil, nil, "", ""
		return fmt.Errorf("%w: %s not readable", ErrNoSessionKey, c.cfg.SessionKeyFile)
	}
	if mnemonic == c.sessionMnemonic && c.privKey != nil {
		return nil
	}
	if err := c.setKey(mnemonic); err != nil {
		c.privKey, c.addrBytes, c.addrStr, c.sessionMnemonic = nil, nil, "", ""
		return fmt.Errorf("%w: %s: %v", ErrNoSessionKey, c.cfg.SessionKeyFile, err)
	}
	c.sessionMnemonic = mnemonic
	return nil
}

// Session reports whether the client signs through a session key.
func (c *Client) Session() bool { return c.cfg.Granter != "" }

// Address returns the account the daemon acts as: the granter in session
// mode, else the signer. Message creator/operator fields take this value.
func (c *Client) Address() string {
	if c.Session() {
		return c.cfg.Granter
	}
	return c.addrStr
}

// SignerAddress returns the key that signs transactions: the session
// grantee in session mode ("" until a key file is loaded).
func (c *Client) SignerAddress() string {
	c.keyMu.Lock()
	defer c.keyMu.Unlock()
	return c.addrStr
}

// signer snapshots the current key, so a rotation mid-broadcast cannot mix
// two keys into one tx.
func (c *Client) signer() (cryptotypes.PrivKey, string) {
	c.keyMu.Lock()
	defer c.keyMu.Unlock()
	return c.privKey, c.addrStr
}

// BroadcastResult is the SYNC-mode outcome: the CheckTx verdict plus the
// tx hash. CheckTx passing means only that the tx entered the mempool —
// every federation rejection that matters (duplicate content hash, the
// per-peer inbound rate limit, ErrSelfVerification, an unbonded verifier)
// is raised in DeliverTx and is invisible here. Callers that need to know
// whether the state change actually happened must follow up with
// Confirm; OK() alone is not evidence of anything.
type BroadcastResult struct {
	TxHash    string
	Code      uint32
	Codespace string
	RawLog    string
}

// OK reports whether the tx passed CheckTx and entered the mempool. It
// does NOT report whether the tx succeeded — see Confirm.
func (r BroadcastResult) OK() bool { return r.Code == 0 }

// SessionUnusable reports a CheckTx rejection meaning the session key can
// no longer act: no grant for this (granter, grantee) pair, the grant
// expired, or its fee budget or exec cap is spent. Nothing but a new
// session fixes it; the daemon waits for the granter to deliver one.
func (r BroadcastResult) SessionUnusable() bool {
	if r.Codespace != sessiontypes.ModuleName {
		return false
	}
	switch r.Code {
	case sessiontypes.ErrSessionNotFound.ABCICode(),
		sessiontypes.ErrSessionExpired.ABCICode(),
		sessiontypes.ErrSpendLimitExceeded.ABCICode(),
		sessiontypes.ErrExecCountExceeded.ABCICode():
		return true
	}
	return false
}

// TxResult is the DeliverTx outcome of an included transaction.
type TxResult struct {
	TxHash string
	Height int64
	Code   uint32
	RawLog string
	Events []abciEvent
}

// OK reports whether the tx was included AND executed successfully.
func (r TxResult) OK() bool { return r.Code == 0 }

type abciEvent struct {
	Type       string `json:"type"`
	Attributes []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"attributes"`
}

// AttributeValue returns the first value of attrKey on the first event of
// eventType, or "" if absent. Used to recover the content_id the chain
// assigned, which SYNC-mode broadcast cannot report.
func (r TxResult) AttributeValue(eventType, attrKey string) string {
	for _, ev := range r.Events {
		if ev.Type != eventType {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == attrKey {
				return attr.Value
			}
		}
	}
	return ""
}

// ErrTxNotFound is returned by Confirm while a tx is still pending
// inclusion, so callers can distinguish "not yet" from "failed".
var ErrTxNotFound = errors.New("sdaptx: tx not found yet")

// Confirm polls /cosmos/tx/v1beta1/txs/{hash} until the tx is included or
// ctx/timeout expires, then returns its DeliverTx result. This is the only
// way to learn that a broadcast actually took effect: SYNC mode reports
// CheckTx only.
func (c *Client) Confirm(ctx context.Context, txHash string, timeout time.Duration) (TxResult, error) {
	if timeout == 0 {
		timeout = 45 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		res, err := c.tx(ctx, txHash)
		if err == nil {
			return res, nil
		}
		if !errors.Is(err, ErrTxNotFound) {
			return TxResult{}, err
		}
		if time.Now().After(deadline) {
			return TxResult{}, fmt.Errorf("sdaptx: tx %s not included within %s: %w",
				txHash, timeout, ErrTxNotFound)
		}
		select {
		case <-ctx.Done():
			return TxResult{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Client) tx(ctx context.Context, txHash string) (TxResult, error) {
	var out struct {
		TxResponse struct {
			TxHash string      `json:"txhash"`
			Height string      `json:"height"`
			Code   uint32      `json:"code"`
			RawLog string      `json:"raw_log"`
			Events []abciEvent `json:"events"`
		} `json:"tx_response"`
	}
	if err := c.GetJSON(ctx, "/cosmos/tx/v1beta1/txs/"+txHash, &out); err != nil {
		// The LCD answers 404/500 for a hash it has not indexed yet.
		if isNotFound(err) {
			return TxResult{}, ErrTxNotFound
		}
		return TxResult{}, err
	}
	if out.TxResponse.TxHash == "" {
		return TxResult{}, ErrTxNotFound
	}
	height, _ := strconv.ParseInt(out.TxResponse.Height, 10, 64)
	return TxResult{
		TxHash: out.TxResponse.TxHash,
		Height: height,
		Code:   out.TxResponse.Code,
		RawLog: out.TxResponse.RawLog,
		Events: out.TxResponse.Events,
	}, nil
}

func isNotFound(err error) bool {
	s := err.Error()
	return strings.Contains(s, "status 404") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "NotFound")
}

// wrapSession packs msgs into one MsgExecSession for the granter, and
// returns the gas limit and fee to use for it.
func (c *Client) wrapSession(grantee string, msgs []sdk.Msg) ([]sdk.Msg, uint64, int64, error) {
	anys := make([]*codectypes.Any, 0, len(msgs))
	for _, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("sdaptx: pack %T: %w", m, err)
		}
		anys = append(anys, a)
	}
	gas := c.cfg.GasLimit + SessionGasOverhead
	// ceil(fee * gas / base gas): the configured fee is sized to the
	// configured gas at the node's minimum price
	fee := (c.cfg.FeeAmount*int64(gas) + int64(c.cfg.GasLimit) - 1) / int64(c.cfg.GasLimit)
	return []sdk.Msg{&sessiontypes.MsgExecSession{
		Grantee: grantee,
		Granter: c.cfg.Granter,
		Msgs:    anys,
	}}, gas, fee, nil
}

// SignAndBroadcast signs msgs and broadcasts in SYNC mode; in session mode
// they go out inside one MsgExecSession signed by the session key. Sequence
// errors (concurrent submissions racing the account query) surface as code
// 32 (sequence mismatch) — callers retry.
func (c *Client) SignAndBroadcast(ctx context.Context, msgs ...sdk.Msg) (BroadcastResult, error) {
	gas, fee := c.cfg.GasLimit, c.cfg.FeeAmount
	if c.Session() {
		if err := c.loadSessionKey(); err != nil {
			return BroadcastResult{}, err
		}
	}
	privKey, addr := c.signer()
	if c.Session() {
		wrapped, g, f, err := c.wrapSession(addr, msgs)
		if err != nil {
			return BroadcastResult{}, err
		}
		msgs, gas, fee = wrapped, g, f
	}
	acctNum, seq, err := c.account(ctx, addr)
	if err != nil {
		return BroadcastResult{}, err
	}

	builder := c.txConfig.NewTxBuilder()
	if err := builder.SetMsgs(msgs...); err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: set msgs: %w", err)
	}
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(c.cfg.Denom, math.NewInt(fee))))
	builder.SetGasLimit(gas)

	// Two-pass signing: first attach an empty signature so the auth info
	// is complete, then sign the sign-bytes it induces.
	emptySig := signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode: signing.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: seq,
	}
	if err := builder.SetSignatures(emptySig); err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: set empty signature: %w", err)
	}
	signerData := authsigning.SignerData{
		Address:       addr,
		ChainID:       c.cfg.ChainID,
		AccountNumber: acctNum,
		Sequence:      seq,
		PubKey:        privKey.PubKey(),
	}
	signBytes, err := authsigning.GetSignBytesAdapter(ctx, c.txConfig.SignModeHandler(),
		signing.SignMode_SIGN_MODE_DIRECT, signerData, builder.GetTx())
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: sign bytes: %w", err)
	}
	sigBytes, err := privKey.Sign(signBytes)
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: sign: %w", err)
	}
	fullSig := signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: sigBytes,
		},
		Sequence: seq,
	}
	if err := builder.SetSignatures(fullSig); err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: set signature: %w", err)
	}
	txBytes, err := c.txConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: encode tx: %w", err)
	}

	payload := map[string]any{
		"tx_bytes": base64.StdEncoding.EncodeToString(txBytes),
		"mode":     "BROADCAST_MODE_SYNC",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return BroadcastResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.LCD+"/cosmos/tx/v1beta1/txs", bytes.NewReader(body))
	if err != nil {
		return BroadcastResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: broadcast: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return BroadcastResult{}, fmt.Errorf("sdaptx: broadcast status %d: %s", resp.StatusCode, truncate(raw, 400))
	}
	var out struct {
		TxResponse struct {
			TxHash    string `json:"txhash"`
			Code      uint32 `json:"code"`
			Codespace string `json:"codespace"`
			RawLog    string `json:"raw_log"`
		} `json:"tx_response"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: decode broadcast response: %w (%s)", err, truncate(raw, 400))
	}
	return BroadcastResult{
		TxHash:    out.TxResponse.TxHash,
		Code:      out.TxResponse.Code,
		Codespace: out.TxResponse.Codespace,
		RawLog:    out.TxResponse.RawLog,
	}, nil
}

// GetJSON issues a GET against the LCD and decodes the JSON response
// into out. Exposed for the daemons' query needs.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.LCD+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sdaptx: get %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sdaptx: get %s: status %d: %s", path, resp.StatusCode, truncate(raw, 400))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("sdaptx: get %s: decode: %w", path, err)
	}
	return nil
}

// account fetches the signer's account number and current sequence.
func (c *Client) account(ctx context.Context, addr string) (acctNum, seq uint64, err error) {
	var out struct {
		Account struct {
			AccountNumber string `json:"account_number"`
			Sequence      string `json:"sequence"`
		} `json:"account"`
	}
	if err := c.GetJSON(ctx, "/cosmos/auth/v1beta1/accounts/"+addr, &out); err != nil {
		return 0, 0, err
	}
	// An absent account answers 404, which GetJSON already turns into an
	// error, so reaching here means the account exists. account_number 0
	// is a legitimate value (the chain's first account), so it must not
	// be treated as "not found" — doing so made genesis accounts unable
	// to sign at all.
	if out.Account.AccountNumber == "" {
		return 0, 0, fmt.Errorf("sdaptx: account %s not found on %s (fund it or check the key)",
			addr, c.cfg.ChainID)
	}
	acctNum, err = strconv.ParseUint(out.Account.AccountNumber, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("sdaptx: parse account_number %q: %w", out.Account.AccountNumber, err)
	}
	if out.Account.Sequence != "" {
		seq, err = strconv.ParseUint(out.Account.Sequence, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("sdaptx: parse sequence %q: %w", out.Account.Sequence, err)
		}
	}
	return acctNum, seq, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// SDK root-codespace error codes the daemons reason about. Retrying a
// non-transient rejection just burns fees: code 13 in particular is
// ErrInsufficientFee (a fixed fee too low for the node's minimum, which a
// retry cannot fix), NOT "mempool full" — that is code 20.
const (
	CodeTxUnauthorized   uint32 = 4  // ErrUnauthorized
	CodeInsufficientFee  uint32 = 13 // ErrInsufficientFee — raise the fee, do not retry
	CodeWrongSequence    uint32 = 32 // ErrWrongSequence — transient, retry
	CodeMempoolIsFull    uint32 = 20 // ErrMempoolIsFull — transient, retry
	CodeTxInMempoolCache uint32 = 19 // ErrTxInMempoolCache — already submitted
)

// Retryable reports whether a CheckTx rejection is worth resubmitting.
// Only the genuinely transient conditions qualify: a sequence race with
// our own previous broadcast, and a full mempool.
func Retryable(code uint32) bool {
	return code == CodeWrongSequence || code == CodeMempoolIsFull
}
