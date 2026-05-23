package genesisinit_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/identity/genesisinit"
	identitymodule "sparkdream/x/identity/module"
	"sparkdream/x/identity/types"
)

// testCodec returns a codec that understands the identity GenesisState's
// gogoproto JSON conventions (int64 as JSON string, etc).
func testCodec() codec.Codec {
	encCfg := moduletestutil.MakeTestEncodingConfig(identitymodule.AppModule{})
	return encCfg.Codec
}

func validID() types.ChainIdentity {
	return types.ChainIdentity{
		ChainHumanName:       "Phoenix",
		ChainTickerPrefix:    "PHX",
		BondDenom:            "upspk.phoenix",
		BondDisplaySymbol:    "PSPK",
		BondDisplayName:      "Phoenix Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           "udream.phoenix",
		DreamDisplaySymbol:   "PDRM",
		DreamDisplayName:     "Phoenix Dream",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}

// genState marshals a typical app_state with identity + staking + mint +
// crisis + bank present, plus arbitrary modules that contain sentinels.
func genState(id types.ChainIdentity, withBankMeta []banktypes.Metadata, withGenutilSentinel bool) json.RawMessage {
	gs := types.GenesisState{Identity: id, AllowChainIdMismatch: false}
	idRaw, _ := json.Marshal(gs)

	staking := map[string]interface{}{
		"params": map[string]string{"bond_denom": types.BondDenomSentinel},
	}
	mint := map[string]interface{}{
		"params": map[string]string{"mint_denom": types.BondDenomSentinel},
	}
	crisis := map[string]interface{}{
		"constant_fee": map[string]interface{}{"denom": types.BondDenomSentinel, "amount": "1000"},
	}
	rep := map[string]interface{}{
		"params": map[string]string{"dream_denom": types.DreamDenomSentinel},
	}

	bank := map[string]interface{}{
		"params": map[string]interface{}{"send_enabled": []interface{}{}},
	}
	if withBankMeta != nil {
		bank["denom_metadata"] = withBankMeta
	}
	if withGenutilSentinel {
		// Embed sentinel literal inside genutil
		bank["balances"] = []interface{}{}
	}

	appState := map[string]interface{}{
		"identity": json.RawMessage(idRaw),
		"staking":  staking,
		"mint":     mint,
		"crisis":   crisis,
		"rep":      rep,
		"bank":     bank,
	}
	if withGenutilSentinel {
		appState["genutil"] = map[string]interface{}{"gen_txs": []string{"sentinel-" + types.BondDenomSentinel}}
	}
	out, _ := json.Marshal(appState)
	return out
}

func TestRewriteSentinelsHappyPath(t *testing.T) {
	out, resolved, err := genesisinit.RewriteSentinels(testCodec(), genState(validID(), nil, false))
	require.NoError(t, err)
	require.Equal(t, "upspk.phoenix", resolved.BondDenom)

	var as map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &as))
	require.NotContains(t, string(out), types.BondDenomSentinel)
	require.NotContains(t, string(out), types.DreamDenomSentinel)

	// staking.params.bond_denom must now be "upspk.phoenix"
	require.Contains(t, string(as["staking"]), `"bond_denom":"upspk.phoenix"`)
	require.Contains(t, string(as["mint"]), `"mint_denom":"upspk.phoenix"`)
	require.Contains(t, string(as["rep"]), `"dream_denom":"udream.phoenix"`)
}

func TestRewriteSentinelsHaltsOnGenutilSentinel(t *testing.T) {
	_, _, err := genesisinit.RewriteSentinels(testCodec(), genState(validID(), nil, true))
	require.Error(t, err)
	require.Contains(t, err.Error(), "genutil")
}

func TestRewriteSentinelsRejectsInvalidIdentity(t *testing.T) {
	bad := validID()
	bad.BondDenom = "uspark"
	_, _, err := genesisinit.RewriteSentinels(testCodec(), genState(bad, nil, false))
	require.Error(t, err)
}

func TestRewriteSentinelsPermissiveOnMissingIdentity(t *testing.T) {
	// V1 deviation: missing identity becomes a no-op so legacy single-chain
	// deployments keep working during migration. See
	// docs/x-identity-implementation-decisions.md.
	appState := map[string]interface{}{
		"staking": map[string]interface{}{"params": map[string]string{"bond_denom": types.BondDenomSentinel}},
	}
	raw, _ := json.Marshal(appState)
	out, resolved, err := genesisinit.RewriteSentinels(testCodec(), raw)
	require.NoError(t, err)
	// No-op: input == output, and resolved identity is zero-valued.
	require.Equal(t, []byte(raw), []byte(out))
	require.Equal(t, types.ChainIdentity{}, resolved)
}

func TestPurgeLegacyBankMetadataRemovesOnlyLegacy(t *testing.T) {
	meta := []banktypes.Metadata{
		{Base: "uspark", Symbol: "SPARK", Display: "spark", Name: "Spark"},
		{Base: "dream", Symbol: "DREAM", Display: "dream", Name: "Dream"},
		{Base: "stake", Symbol: "STAKE", Display: "stake", Name: "Stake"},
		{Base: "uatom", Symbol: "ATOM", Display: "atom", Name: "Atom"},
		{Base: "upspk.phoenix", Symbol: "PSPK", Display: "pspk", Name: "Phoenix Spark"},
	}
	out, _, err := genesisinit.RewriteSentinels(testCodec(), genState(validID(), meta, false))
	require.NoError(t, err)
	var as map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &as))
	var bank map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(as["bank"], &bank))
	var kept []banktypes.Metadata
	require.NoError(t, json.Unmarshal(bank["denom_metadata"], &kept))
	// uatom and upspk.phoenix should survive; uspark, dream, stake purged.
	bases := make([]string, 0, len(kept))
	for _, m := range kept {
		bases = append(bases, m.Base)
	}
	require.ElementsMatch(t, []string{"uatom", "upspk.phoenix"}, bases)
}

func TestPurgeLegacyBankMetadataPreservesIfChosen(t *testing.T) {
	id := validID()
	id.BondDenom = "upspk.uspark" // weird degenerate-but-legal choice
	// Force a chosen-denom that also matches a legacy literal? Both regexes
	// reject "uspark" as bond_denom (no dot). So the actual degenerate case
	// is purposely contrived: pick a value that survives validation AND is
	// in the legacy list. There is none — the regex blocks them. So this
	// test instead asserts that a legitimate non-legacy choice doesn't get
	// purged.
	meta := []banktypes.Metadata{
		{Base: "upspk.uspark", Symbol: "SPK", Display: "spk", Name: "Spark"},
	}
	out, _, err := genesisinit.RewriteSentinels(testCodec(), genState(id, meta, false))
	require.NoError(t, err)
	require.Contains(t, string(out), "upspk.uspark")
}

func TestRewriteSentinelsPreservesUnrelatedFields(t *testing.T) {
	id := validID()
	// Build a genesis that includes arbitrary unrelated content; the rewrite
	// should leave it untouched (except for sentinel substitutions).
	gs := types.GenesisState{Identity: id}
	idRaw, _ := json.Marshal(gs)
	appState := map[string]interface{}{
		"identity": json.RawMessage(idRaw),
		"random": map[string]interface{}{
			"value":  "untouched",
			"number": 42,
		},
	}
	raw, _ := json.Marshal(appState)
	out, _, err := genesisinit.RewriteSentinels(testCodec(), raw)
	require.NoError(t, err)
	require.Contains(t, string(out), "untouched")
	require.Contains(t, string(out), `"number":42`)
}

func TestRewriteIdempotent(t *testing.T) {
	in := genState(validID(), nil, false)
	out, _, err := genesisinit.RewriteSentinels(testCodec(), in)
	require.NoError(t, err)
	// Running it again on already-rewritten state succeeds (no sentinels
	// left).
	out2, _, err := genesisinit.RewriteSentinels(testCodec(), out)
	require.NoError(t, err)
	// Normalize via json round-trip (whitespace/key-order can differ).
	require.True(t, jsonEqual(out, out2))
}

func jsonEqual(a, b json.RawMessage) bool {
	var av, bv interface{}
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	ar, _ := json.Marshal(av)
	br, _ := json.Marshal(bv)
	return strings.EqualFold(string(ar), string(br))
}
