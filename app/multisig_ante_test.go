package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	sessionkeeper "sparkdream/x/session/keeper"
	sessiontypes "sparkdream/x/session/types"
)

// Native k-of-n multisig accounts (LegacyAminoPubKey) are a supported account
// type (docs/multisig-accounts.md). These tests run real txs through the full
// ante chain and FinalizeBlock on an e2e snapshot genesis: a 2-of-3 account
// sends SPARK and creates a session key with mixed sub-signature modes, a
// 1-of-3 tx and a tx carrying a foreign sub-signature are rejected, and a
// 3-of-5 tx costs only a few thousand gas more than a single-key one, so
// multisig voters stay inside the council-vote gas headroom.

const multisigTestFund = "100000000000"

type msAccount struct {
	keys   []cryptotypes.PrivKey
	pub    *kmultisig.LegacyAminoPubKey
	addr   sdk.AccAddress
	accNum uint64
	seq    uint64
}

func newMsAccount(k, n int) *msAccount {
	keys := make([]cryptotypes.PrivKey, n)
	pubs := make([]cryptotypes.PubKey, n)
	for i := range keys {
		keys[i] = secp256k1.GenPrivKey()
		pubs[i] = keys[i].PubKey()
	}
	pub := kmultisig.NewLegacyAminoPubKey(k, pubs)
	return &msAccount{keys: keys, pub: pub, addr: sdk.AccAddress(pub.Address())}
}

// genesisWithAccounts loads the session e2e snapshot genesis and adds funded
// base accounts for every address given.
func genesisWithAccounts(t *testing.T, addrs ...sdk.AccAddress) (*genutiltypes.AppGenesis, string) {
	t.Helper()
	files := snapshotGenesisFiles(t)
	require.NotEmpty(t, files, "no e2e snapshots under test/*/snapshots/post-setup")
	genFile := files[0]
	for _, f := range files {
		if len(f) > len("session") && containsSuite(f, "session") {
			genFile = f
		}
	}
	gen, err := genutiltypes.AppGenesisFromFile(genFile)
	require.NoError(t, err)

	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(gen.AppState, &state))

	var staking map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(state["staking"], &staking))
	var stakingParams map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(staking["params"], &stakingParams))
	var denom string
	require.NoError(t, json.Unmarshal(stakingParams["bond_denom"], &denom))

	var auth map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(state["auth"], &auth))
	var accounts []json.RawMessage
	require.NoError(t, json.Unmarshal(auth["accounts"], &accounts))
	var bank map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(state["bank"], &bank))
	var balances []json.RawMessage
	require.NoError(t, json.Unmarshal(bank["balances"], &balances))
	var supply []sdk.Coin
	require.NoError(t, json.Unmarshal(bank["supply"], &supply))

	fund, ok := sdkmath.NewIntFromString(multisigTestFund)
	require.True(t, ok)
	added := sdk.NewCoins()
	for _, a := range addrs {
		accounts = append(accounts, json.RawMessage(fmt.Sprintf(
			`{"@type":"/cosmos.auth.v1beta1.BaseAccount","address":%q,"pub_key":null,"account_number":"0","sequence":"0"}`, a.String())))
		balances = append(balances, json.RawMessage(fmt.Sprintf(
			`{"address":%q,"coins":[{"denom":%q,"amount":%q}]}`, a.String(), denom, multisigTestFund)))
		added = added.Add(sdk.NewCoin(denom, fund))
	}
	supply = sdk.Coins(supply).Add(added...)

	var err2 error
	auth["accounts"], err2 = json.Marshal(accounts)
	require.NoError(t, err2)
	bank["balances"], err2 = json.Marshal(balances)
	require.NoError(t, err2)
	bank["supply"], err2 = json.Marshal(supply)
	require.NoError(t, err2)
	state["auth"], err2 = json.Marshal(auth)
	require.NoError(t, err2)
	state["bank"], err2 = json.Marshal(bank)
	require.NoError(t, err2)
	gen.AppState, err2 = json.Marshal(state)
	require.NoError(t, err2)
	return gen, denom
}

func containsSuite(path, suite string) bool {
	return len(path) >= len(suite) && (func() bool {
		for i := 0; i+len(suite) <= len(path); i++ {
			if path[i:i+len(suite)] == suite {
				return true
			}
		}
		return false
	})()
}

type multisigHarness struct {
	t      *testing.T
	app    *App
	gen    *genutiltypes.AppGenesis
	denom  string
	height int64
}

func newMultisigHarness(t *testing.T, accs ...*msAccount) *multisigHarness {
	t.Helper()
	addrs := make([]sdk.AccAddress, 0, len(accs))
	for _, a := range accs {
		addrs = append(addrs, a.addr)
	}
	gen, denom := genesisWithAccounts(t, addrs...)

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()
	app := New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(gen.ChainID))

	var consensus *cmtproto.ConsensusParams
	if gen.Consensus != nil && gen.Consensus.Params != nil {
		p := gen.Consensus.Params.ToProto()
		consensus = &p
	}
	_, err := app.InitChain(&abci.RequestInitChain{
		ChainId: gen.ChainID, Time: gen.GenesisTime, AppStateBytes: gen.AppState,
		ConsensusParams: consensus, InitialHeight: gen.InitialHeight,
	})
	require.NoError(t, err)
	h := &multisigHarness{t: t, app: app, gen: gen, denom: denom, height: gen.InitialHeight}
	if h.height == 0 {
		h.height = 1
	}
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: h.height, Time: h.blockTime()})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	ctx := app.NewContextLegacy(true, cmtproto.Header{Height: h.height, ChainID: gen.ChainID, Time: h.blockTime()})
	for _, a := range accs {
		acc := app.AuthKeeper.GetAccount(ctx, a.addr)
		require.NotNil(t, acc, "genesis account %s missing", a.addr)
		a.accNum = acc.GetAccountNumber()
		a.seq = acc.GetSequence()
	}
	return h
}

func (h *multisigHarness) blockTime() time.Time {
	return h.gen.GenesisTime.Add(time.Duration(h.height) * 6 * time.Second)
}

// deliver runs one tx in its own block and returns its result.
func (h *multisigHarness) deliver(txBytes []byte) *abci.ExecTxResult {
	h.t.Helper()
	h.height++
	res, err := h.app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: h.height, Time: h.blockTime(), Txs: [][]byte{txBytes}})
	require.NoError(h.t, err)
	_, err = h.app.Commit()
	require.NoError(h.t, err)
	require.Len(h.t, res.TxResults, 1)
	return res.TxResults[0]
}

type subSig struct {
	index int
	mode  signing.SignMode
	key   cryptotypes.PrivKey // nil = the account's own key at index
}

// buildMultisigTx signs msgs for acc with the given sub-signatures. Direct-mode
// sign bytes cover auth_info, whose multisig ModeInfo bitarray depends on which
// signers take part, so the final bitarray is established with placeholder
// signatures first and every real signature is computed against it.
func (h *multisigHarness) buildMultisigTx(acc *msAccount, gas uint64, subs []subSig, msgs ...sdk.Msg) []byte {
	h.t.Helper()
	txConfig := h.app.TxConfig()
	txb := txConfig.NewTxBuilder()
	require.NoError(h.t, txb.SetMsgs(msgs...))
	txb.SetGasLimit(gas)
	txb.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(h.denom, int64(gas)))) // 1 denom-unit per gas: above any min price

	n := len(acc.pub.GetPubKeys())
	data := multisig.NewMultisig(n)
	placeholder := make([]byte, 64)
	for _, s := range subs {
		require.NoError(h.t, multisig.AddSignatureFromPubKey(data,
			&signing.SingleSignatureData{SignMode: s.mode, Signature: placeholder},
			acc.pub.GetPubKeys()[s.index], acc.pub.GetPubKeys()))
	}
	require.NoError(h.t, txb.SetSignatures(signing.SignatureV2{PubKey: acc.pub, Data: data, Sequence: acc.seq}))

	ctx := h.app.NewContextLegacy(true, cmtproto.Header{Height: h.height, ChainID: h.gen.ChainID, Time: h.blockTime()})
	for _, s := range subs {
		key := acc.keys[s.index]
		if s.key != nil {
			key = s.key
		}
		signerData := authsigning.SignerData{
			ChainID: h.gen.ChainID, AccountNumber: acc.accNum, Sequence: acc.seq,
			PubKey: acc.pub.GetPubKeys()[s.index], Address: acc.addr.String(),
		}
		bytesToSign, err := authsigning.GetSignBytesAdapter(ctx, txConfig.SignModeHandler(), s.mode, signerData, txb.GetTx())
		require.NoError(h.t, err)
		sig, err := key.Sign(bytesToSign)
		require.NoError(h.t, err)
		require.NoError(h.t, multisig.AddSignatureFromPubKey(data,
			&signing.SingleSignatureData{SignMode: s.mode, Signature: sig},
			acc.pub.GetPubKeys()[s.index], acc.pub.GetPubKeys()))
	}
	require.NoError(h.t, txb.SetSignatures(signing.SignatureV2{PubKey: acc.pub, Data: data, Sequence: acc.seq}))
	bz, err := txConfig.TxEncoder()(txb.GetTx())
	require.NoError(h.t, err)
	return bz
}

func (h *multisigHarness) buildSingleTx(key cryptotypes.PrivKey, accNum, seq, gas uint64, msgs ...sdk.Msg) []byte {
	h.t.Helper()
	txConfig := h.app.TxConfig()
	txb := txConfig.NewTxBuilder()
	require.NoError(h.t, txb.SetMsgs(msgs...))
	txb.SetGasLimit(gas)
	txb.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(h.denom, int64(gas))))
	mode := signing.SignMode_SIGN_MODE_DIRECT
	require.NoError(h.t, txb.SetSignatures(signing.SignatureV2{PubKey: key.PubKey(), Data: &signing.SingleSignatureData{SignMode: mode}, Sequence: seq}))
	ctx := h.app.NewContextLegacy(true, cmtproto.Header{Height: h.height, ChainID: h.gen.ChainID, Time: h.blockTime()})
	signerData := authsigning.SignerData{ChainID: h.gen.ChainID, AccountNumber: accNum, Sequence: seq, PubKey: key.PubKey(), Address: sdk.AccAddress(key.PubKey().Address()).String()}
	bytesToSign, err := authsigning.GetSignBytesAdapter(ctx, txConfig.SignModeHandler(), mode, signerData, txb.GetTx())
	require.NoError(h.t, err)
	sig, err := key.Sign(bytesToSign)
	require.NoError(h.t, err)
	require.NoError(h.t, txb.SetSignatures(signing.SignatureV2{PubKey: key.PubKey(), Data: &signing.SingleSignatureData{SignMode: mode, Signature: sig}, Sequence: seq}))
	bz, err := txConfig.TxEncoder()(txb.GetTx())
	require.NoError(h.t, err)
	return bz
}

func TestMultisigAccountThroughAnteChain(t *testing.T) {
	ms := newMsAccount(2, 3)
	hot := secp256k1.GenPrivKey()
	hotAddr := sdk.AccAddress(hot.PubKey().Address())
	h := newMultisigHarness(t, ms)

	send := &banktypes.MsgSend{FromAddress: ms.addr.String(), ToAddress: hotAddr.String(), Amount: sdk.NewCoins(sdk.NewInt64Coin(h.denom, 1_000_000))}
	direct, amino := signing.SignMode_SIGN_MODE_DIRECT, signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON

	t.Run("1-of-3 is rejected", func(t *testing.T) {
		res := h.deliver(h.buildMultisigTx(ms, 200000, []subSig{{index: 0, mode: amino}}, send))
		require.NotZero(t, res.Code, res.Log)
	})
	t.Run("foreign sub-signature is rejected", func(t *testing.T) {
		res := h.deliver(h.buildMultisigTx(ms, 200000, []subSig{{index: 0, mode: amino, key: secp256k1.GenPrivKey()}, {index: 2, mode: amino}}, send))
		require.NotZero(t, res.Code, res.Log)
	})
	t.Run("2-of-3 bank send with mixed sign modes", func(t *testing.T) {
		res := h.deliver(h.buildMultisigTx(ms, 200000, []subSig{{index: 0, mode: direct}, {index: 2, mode: amino}}, send))
		require.Zero(t, res.Code, res.Log)
		ms.seq++
		ctx := h.app.NewContextLegacy(true, cmtproto.Header{Height: h.height, ChainID: h.gen.ChainID, Time: h.blockTime()})
		require.Equal(t, int64(1_000_000), h.app.BankKeeper.GetBalance(ctx, hotAddr, h.denom).Amount.Int64())
		acc := h.app.AuthKeeper.GetAccount(ctx, ms.addr)
		_, isMulti := acc.GetPubKey().(*kmultisig.LegacyAminoPubKey)
		require.True(t, isMulti, "SetPubKeyDecorator stored the multisig pubkey")
	})
	t.Run("2-of-3 creates a session key for a hot key (cold root, hot key pattern)", func(t *testing.T) {
		params := sessiontypes.DefaultParams()
		require.NotEmpty(t, params.AllowedMsgTypes)
		grant := &sessiontypes.MsgCreateGrant{
			Granter:   ms.addr.String(),
			Grantee:   hotAddr.String(),
			ExpiresAt: h.blockTime().Add(24 * time.Hour),
			Note:      "hot key",
			Payload: &sessiontypes.MsgCreateGrant_SessionKey{SessionKey: &sessiontypes.SessionKeyPayload{
				AllowedMsgTypes: []string{params.AllowedMsgTypes[0]},
				SpendLimit:      sdk.NewInt64Coin(h.denom, 10_000_000),
				MaxExecCount:    10,
			}},
		}
		res := h.deliver(h.buildMultisigTx(ms, 300000, []subSig{{index: 1, mode: amino}, {index: 2, mode: direct}}, grant))
		require.Zero(t, res.Code, res.Log)
		ms.seq++
		ctx := h.app.NewContextLegacy(true, cmtproto.Header{Height: h.height, ChainID: h.gen.ChainID, Time: h.blockTime()})
		grants, err := sessionkeeper.NewQueryServerImpl(h.app.SessionKeeper).GrantsByGrantee(ctx, &sessiontypes.QueryGrantsByGranteeRequest{Grantee: hotAddr.String()})
		require.NoError(t, err)
		require.Len(t, grants.Grants, 1)
		require.Equal(t, ms.addr.String(), grants.Grants[0].Granter)
	})
	t.Run("stale sequence after the real one is rejected", func(t *testing.T) {
		stale := *ms
		stale.seq = 0
		res := h.deliver(h.buildMultisigTx(&stale, 200000, []subSig{{index: 0, mode: amino}, {index: 1, mode: amino}}, send))
		require.NotZero(t, res.Code, res.Log)
	})
}

// TestMultisigGasOverheadIsBounded measures the steady-state gas a 3-of-5
// multisig tx costs over a single-key tx with the same message: the extra
// signatures (1000 gas each), the five pubkeys and three signatures in the tx
// bytes (10 gas per byte) and the larger account record. The overhead is about
// 25k (14k for a 2-of-3), so a multisig council voter fits the default gas
// limit but uses half of the ~50k vote headroom; docs/multisig-accounts.md
// tells multisig signers to pass --gas 300000. The first tx of an account also
// pays to store its pubkey and is not what this pins.
func TestMultisigGasOverheadIsBounded(t *testing.T) {
	ms := newMsAccount(3, 5)
	single := newMsAccount(1, 1) // one plain key, funded the same way (its address is the key's, not a multisig's)
	single.addr = sdk.AccAddress(single.keys[0].PubKey().Address())
	h := newMultisigHarness(t, ms, single)
	sink := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	amino := signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON
	subs := []subSig{{index: 0, mode: amino}, {index: 2, mode: amino}, {index: 4, mode: amino}}
	sendMs := &banktypes.MsgSend{FromAddress: ms.addr.String(), ToAddress: sink.String(), Amount: sdk.NewCoins(sdk.NewInt64Coin(h.denom, 1))}
	sendSingle := &banktypes.MsgSend{FromAddress: single.addr.String(), ToAddress: sink.String(), Amount: sdk.NewCoins(sdk.NewInt64Coin(h.denom, 1))}

	// Warm-up: stores both pubkeys and creates the sink account.
	for _, bz := range [][]byte{
		h.buildSingleTx(single.keys[0], single.accNum, single.seq, 200000, sendSingle),
		h.buildMultisigTx(ms, 200000, subs, sendMs),
	} {
		res := h.deliver(bz)
		require.Zero(t, res.Code, res.Log)
	}
	single.seq++
	ms.seq++

	resMs := h.deliver(h.buildMultisigTx(ms, 200000, subs, sendMs))
	require.Zero(t, resMs.Code, resMs.Log)
	resSingle := h.deliver(h.buildSingleTx(single.keys[0], single.accNum, single.seq, 200000, sendSingle))
	require.Zero(t, resSingle.Code, resSingle.Log)

	overhead := resMs.GasUsed - resSingle.GasUsed
	t.Logf("steady-state gas: 3-of-5 multisig %d, single key %d, overhead %d", resMs.GasUsed, resSingle.GasUsed, overhead)
	require.Greater(t, overhead, int64(3*1000), "at least the three extra signature verifications")
	require.Less(t, overhead, int64(35_000), "3-of-5 overhead must leave room inside the ~50k council-vote headroom")
	require.Less(t, resMs.GasUsed, int64(200_000))
}
