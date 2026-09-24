// Package sdaptx is the thin LCD-based transaction client shared by the
// federation daemons (cmd/sdapbridge, cmd/sdapverify). It signs with a
// raw key derived from a mnemonic using the chain's standard coin type
// (118, path m/44'/118'/0'/0/0) and SIGN_MODE_DIRECT, then broadcasts
// through the LCD's /cosmos/tx/v1beta1/txs endpoint in SYNC mode.
//
// Both daemons could sign through a session key (the federation daemon
// messages are allowlisted in x/session genesis since the P0.2 change);
// wrapping Msgs in MsgExecSession is deliberately NOT implemented yet —
// the devnet reset that seeds the ceiling has not happened (P3.0), so
// v1 signs with the operator/verifier key directly and accepts that
// risk on devnet only. The Signer seam below is where a
// session wrapper slots in later.
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
	"strconv"
	"strings"
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
	// Mnemonic of the signing account (BIP39, 12/24 words).
	Mnemonic string
}

// Client signs and broadcasts through an LCD endpoint.
type Client struct {
	cfg       Config
	http      *http.Client
	txConfig  client.TxConfig
	privKey   cryptotypes.PrivKey
	addrBytes []byte
	addrStr   string
}

// New derives the key, sets the bech32 prefix, and registers the minimal
// codec surface the federation messages need.
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
	cdc := codec.NewProtoCodec(ir)
	txConfig := authtx.NewTxConfig(cdc, authtx.DefaultSignModes)

	if !bip39.IsMnemonicValid(cfg.Mnemonic) {
		return nil, fmt.Errorf("sdaptx: invalid mnemonic")
	}
	derived, err := hd.Secp256k1.Derive()(cfg.Mnemonic, "", hd.CreateHDPath(118, 0, 0).String())
	if err != nil {
		return nil, fmt.Errorf("sdaptx: derive key from mnemonic: %w", err)
	}
	priv := &secp256k1.PrivKey{Key: derived}
	addr := priv.PubKey().Address()
	// sdk.Bech32ifyAddressBytes, not btcutil/bech32.Encode: the latter
	// expects data already regrouped into 5-bit words and errors out
	// ("invalid data byte") on a raw 20-byte address for essentially every
	// key. The SDK helper does the ConvertBits(8, 5) first.
	addrStr, err := sdk.Bech32ifyAddressBytes(cfg.Bech32Prefix, addr.Bytes())
	if err != nil {
		return nil, fmt.Errorf("sdaptx: bech32-encode address: %w", err)
	}
	return &Client{
		cfg:       cfg,
		http:      &http.Client{Timeout: 30 * time.Second},
		txConfig:  txConfig,
		privKey:   priv,
		addrBytes: addr.Bytes(),
		addrStr:   addrStr,
	}, nil
}

// Address returns the signer's bech32 address.
func (c *Client) Address() string { return c.addrStr }

// BroadcastResult is the SYNC-mode outcome: the CheckTx verdict plus the
// tx hash. CheckTx passing means only that the tx entered the mempool —
// every federation rejection that matters (duplicate content hash, the
// per-peer inbound rate limit, ErrSelfVerification, an unbonded verifier)
// is raised in DeliverTx and is invisible here. Callers that need to know
// whether the state change actually happened must follow up with
// Confirm; OK() alone is not evidence of anything.
type BroadcastResult struct {
	TxHash string
	Code   uint32
	RawLog string
}

// OK reports whether the tx passed CheckTx and entered the mempool. It
// does NOT report whether the tx succeeded — see Confirm.
func (r BroadcastResult) OK() bool { return r.Code == 0 }

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

// SignAndBroadcast signs msgs with the derived key and broadcasts in
// SYNC mode. Sequence errors (concurrent submissions racing the account
// query) surface as code 32 (sequence mismatch) — callers retry.
func (c *Client) SignAndBroadcast(ctx context.Context, msgs ...sdk.Msg) (BroadcastResult, error) {
	acctNum, seq, err := c.account(ctx)
	if err != nil {
		return BroadcastResult{}, err
	}

	builder := c.txConfig.NewTxBuilder()
	if err := builder.SetMsgs(msgs...); err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: set msgs: %w", err)
	}
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(c.cfg.Denom, math.NewInt(c.cfg.FeeAmount))))
	builder.SetGasLimit(c.cfg.GasLimit)

	// Two-pass signing: first attach an empty signature so the auth info
	// is complete, then sign the sign-bytes it induces.
	emptySig := signing.SignatureV2{
		PubKey: c.privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode: signing.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: seq,
	}
	if err := builder.SetSignatures(emptySig); err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: set empty signature: %w", err)
	}
	signerData := authsigning.SignerData{
		Address:       c.addrStr,
		ChainID:       c.cfg.ChainID,
		AccountNumber: acctNum,
		Sequence:      seq,
		PubKey:        c.privKey.PubKey(),
	}
	signBytes, err := authsigning.GetSignBytesAdapter(ctx, c.txConfig.SignModeHandler(),
		signing.SignMode_SIGN_MODE_DIRECT, signerData, builder.GetTx())
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: sign bytes: %w", err)
	}
	sigBytes, err := c.privKey.Sign(signBytes)
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: sign: %w", err)
	}
	fullSig := signing.SignatureV2{
		PubKey: c.privKey.PubKey(),
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
			TxHash string `json:"txhash"`
			Code   uint32 `json:"code"`
			RawLog string `json:"raw_log"`
		} `json:"tx_response"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return BroadcastResult{}, fmt.Errorf("sdaptx: decode broadcast response: %w (%s)", err, truncate(raw, 400))
	}
	return BroadcastResult{
		TxHash: out.TxResponse.TxHash,
		Code:   out.TxResponse.Code,
		RawLog: out.TxResponse.RawLog,
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
func (c *Client) account(ctx context.Context) (acctNum, seq uint64, err error) {
	var out struct {
		Account struct {
			AccountNumber string `json:"account_number"`
			Sequence      string `json:"sequence"`
		} `json:"account"`
	}
	if err := c.GetJSON(ctx, "/cosmos/auth/v1beta1/accounts/"+c.addrStr, &out); err != nil {
		return 0, 0, err
	}
	// An absent account answers 404, which GetJSON already turns into an
	// error, so reaching here means the account exists. account_number 0
	// is a legitimate value (the chain's first account), so it must not
	// be treated as "not found" — doing so made genesis accounts unable
	// to sign at all.
	if out.Account.AccountNumber == "" {
		return 0, 0, fmt.Errorf("sdaptx: account %s not found on %s (fund it or check the mnemonic)",
			c.addrStr, c.cfg.ChainID)
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
