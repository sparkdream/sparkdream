package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/server"
	"github.com/stretchr/testify/require"

	"sparkdream/x/identity/types"
)

// runValidate runs `genesis identity validate` against a copy of genesis.
func runValidate(t *testing.T, genesis []byte) error {
	t.Helper()
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "genesis.json"), genesis, 0o644))
	serverCtx := server.NewDefaultContext()
	serverCtx.Config.RootDir = home
	cmd := validateCmd()
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))
	cmd.SetArgs(nil)
	cmd.SilenceUsage = true
	return cmd.Execute()
}

// IDN-07: validate reads the committed network geneses, which carry int64
// founded_at as a proto-JSON string (stdlib encoding/json refused it).
func TestValidateCommittedNetworkGeneses(t *testing.T) {
	for _, network := range []string{"mainnet", "testnet", "devnet"} {
		t.Run(network, func(t *testing.T) {
			bz, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "config", "network", network, "genesis.json"))
			if err != nil {
				t.Skipf("%s genesis not generated: %v", network, err)
			}
			require.NoError(t, runValidate(t, bz))
		})
	}
}

// IDN-07: the spec's hard errors for legacy-denom metadata and balances,
// and the pre-init rewrite's refusal of non-canonical native metadata.
func TestValidateRefusesLegacyAndNonCanonicalDenoms(t *testing.T) {
	bz, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "config", "network", "devnet", "genesis.json"))
	if err != nil {
		t.Skipf("devnet genesis not generated: %v", err)
	}
	mutate := func(f func(bank map[string]any, id types.ChainIdentity)) []byte {
		var doc map[string]any
		require.NoError(t, json.Unmarshal(bz, &doc))
		appState := doc["app_state"].(map[string]any)
		idRaw, err := json.Marshal(appState[types.ModuleName])
		require.NoError(t, err)
		var gs types.GenesisState
		require.NoError(t, identityCodec.UnmarshalJSON(idRaw, &gs))
		f(appState["bank"].(map[string]any), gs.Identity)
		out, err := json.Marshal(doc)
		require.NoError(t, err)
		return out
	}

	legacyMeta := mutate(func(bank map[string]any, _ types.ChainIdentity) {
		bank["denom_metadata"] = []any{map[string]any{"base": "uspark", "display": "spark", "symbol": "SPARK", "name": "Spark"}}
	})
	require.ErrorContains(t, runValidate(t, legacyMeta), "legacy denom \"uspark\"")

	legacyBalance := mutate(func(bank map[string]any, _ types.ChainIdentity) {
		bank["balances"] = append(bank["balances"].([]any), map[string]any{
			"address": "sprkdrm1qyqszqgpqyqszqgpqyqszqgpqyqszqgpnkyu7x",
			"coins":   []any{map[string]any{"denom": "stake", "amount": "1"}},
		})
	})
	require.ErrorContains(t, runValidate(t, legacyBalance), "legacy denom \"stake\"")

	lowercase := mutate(func(bank map[string]any, id types.ChainIdentity) {
		spark, _ := id.NativeDenomMetadata()
		spark.Symbol = "sparz"
		raw, err := json.Marshal(spark)
		require.NoError(t, err)
		var entry map[string]any
		require.NoError(t, json.Unmarshal(raw, &entry))
		bank["denom_metadata"] = []any{entry}
	})
	require.ErrorContains(t, runValidate(t, lowercase), "disagrees with the chain identity")
}

// Both denoms derive from their own symbols; nothing is a fixed name.
func TestGenesisIdentityInitDerivesDenoms(t *testing.T) {
	bond, err := deriveDenom("bond", "PSPK", "Phoenix")
	require.NoError(t, err)
	require.Equal(t, "upspk.phoenix", bond)
	dream, err := deriveDenom("dream", "PDRM", "Phoenix")
	require.NoError(t, err)
	require.Equal(t, "updrm.phoenix", dream)

	for _, sym := range []string{"AB", "LONGSYM", "DRM2"} {
		_, err := deriveDenom("dream", sym, "Phoenix")
		require.ErrorContains(t, err, "pass --dream-denom explicitly", sym)
	}
}
