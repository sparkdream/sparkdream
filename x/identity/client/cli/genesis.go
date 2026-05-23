// Package cli implements the `sparkdreamd genesis identity ...` subcommands
// described in §15 of docs/x-identity-spec.md.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"sparkdream/x/identity/types"
)

// GenesisIdentityCmd returns the `genesis identity` parent command.
func GenesisIdentityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identity",
		Short: "Manage the chain's identity record in genesis.json",
	}
	cmd.AddCommand(initCmd())
	cmd.AddCommand(showCmd())
	cmd.AddCommand(validateCmd())
	return cmd
}

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set the chain identity in genesis.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			chainName, _ := cmd.Flags().GetString("chain-name")
			tickerPrefix, _ := cmd.Flags().GetString("ticker-prefix")
			bondSymbol, _ := cmd.Flags().GetString("bond-symbol")
			dreamSymbol, _ := cmd.Flags().GetString("dream-symbol")
			bondDenom, _ := cmd.Flags().GetString("bond-denom")
			dreamDenom, _ := cmd.Flags().GetString("dream-denom")
			decimals, _ := cmd.Flags().GetUint32("decimals")
			confirmDecimals, _ := cmd.Flags().GetBool("confirm-non-default-decimals")
			foundedAt, _ := cmd.Flags().GetInt64("founded-at")
			force, _ := cmd.Flags().GetBool("force")
			iMeanIt, _ := cmd.Flags().GetBool("i-mean-it")
			allowMismatch, _ := cmd.Flags().GetBool("allow-chain-id-mismatch")

			if chainName == "" || tickerPrefix == "" || bondSymbol == "" || dreamSymbol == "" {
				return fmt.Errorf("--chain-name, --ticker-prefix, --bond-symbol, --dream-symbol are required")
			}
			if decimals != 6 && !confirmDecimals {
				return fmt.Errorf("--decimals %d is non-default; pass --confirm-non-default-decimals to acknowledge irreversibility", decimals)
			}
			// Default bond_denom derivation: "u" + lowercase(bond-symbol) + "." +
			// lowercase(chain-name). The bond_denom regex (spec §11) requires 2-5
			// chars between `u` and `.`, so the derivation only works for
			// bond-symbols of length 3-5. Longer symbols must pass --bond-denom
			// explicitly. See implementation-decisions doc.
			if bondDenom == "" {
				if len(bondSymbol) < 3 || len(bondSymbol) > 5 {
					return fmt.Errorf("auto-derivation of --bond-denom requires --bond-symbol of length 3-5 (got %q, length %d); pass --bond-denom explicitly",
						bondSymbol, len(bondSymbol))
				}
				bondDenom = "u" + strings.ToLower(bondSymbol) + "." + strings.ToLower(chainName)
			}
			if dreamDenom == "" {
				dreamDenom = "udream." + strings.ToLower(chainName)
			}
			if foundedAt == 0 {
				foundedAt = time.Now().Unix()
			}

			id := types.ChainIdentity{
				ChainHumanName:       chainName,
				ChainTickerPrefix:    strings.ToUpper(tickerPrefix),
				BondDenom:            bondDenom,
				BondDisplaySymbol:    strings.ToUpper(bondSymbol),
				BondDisplayName:      fmt.Sprintf("%s Spark", chainName),
				BondDisplayDecimals:  decimals,
				DreamDenom:           dreamDenom,
				DreamDisplaySymbol:   strings.ToUpper(dreamSymbol),
				DreamDisplayName:     fmt.Sprintf("%s Dream", chainName),
				DreamDisplayDecimals: decimals,
				FoundedAt:            foundedAt,
			}
			if err := id.Validate(); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			serverCtx := server.GetServerContextFromCmd(cmd)
			genFile, err := genesisFilePath(serverCtx)
			if err != nil {
				return err
			}
			appState, doc, err := loadAppState(genFile)
			if err != nil {
				return err
			}

			if existingRaw, ok := appState[types.ModuleName]; ok && len(existingRaw) > 0 && string(existingRaw) != "null" {
				var existing types.GenesisState
				_ = json.Unmarshal(existingRaw, &existing)
				if existing.Identity.BondDenom != "" {
					if !(force && iMeanIt) {
						return fmt.Errorf("app_state.%s already initialized; pass --force --i-mean-it to overwrite (this is destructive — see docs/x-identity-spec.md §3.4)", types.ModuleName)
					}
					fmt.Fprintln(os.Stderr, "WARNING: overwriting existing chain identity. Any chain that ever ran with the old identity will fail re-import; this is a destructive operation.")
				}
			}

			gs := types.GenesisState{Identity: id, AllowChainIdMismatch: allowMismatch}
			gsRaw, err := json.Marshal(gs)
			if err != nil {
				return err
			}
			appState[types.ModuleName] = gsRaw

			// Side-effect: wire sentinels into common SDK module params so a
			// fresh-default genesis becomes valid after the sentinel rewrite
			// runs at chain start.
			if err := wireSDKSentinels(appState); err != nil {
				return err
			}

			return saveAppState(genFile, doc, appState)
		},
	}
	cmd.Flags().String("chain-name", "", "Human-readable chain name (e.g., Phoenix)")
	cmd.Flags().String("ticker-prefix", "", "Uppercase ticker prefix (e.g., PHX)")
	cmd.Flags().String("bond-symbol", "", "Wallet ticker for the bond token (e.g., PSPK)")
	cmd.Flags().String("dream-symbol", "", "Wallet ticker for the DREAM token (e.g., PDRM)")
	cmd.Flags().String("bond-denom", "", "Override derived bond denom; default u<lowercase-bond-symbol>.<chainname>; required if bond-symbol is longer than 5 chars")
	cmd.Flags().String("dream-denom", "", "Override derived dream denom; default udream.<chainname>")
	cmd.Flags().Uint32("decimals", 6, "Display decimals for both tokens (6 is the Cosmos convention)")
	cmd.Flags().Bool("confirm-non-default-decimals", false, "Required when --decimals is not 6")
	cmd.Flags().Int64("founded-at", 0, "Founding unix seconds; defaults to current time")
	cmd.Flags().Bool("force", false, "Allow overwrite of existing identity (requires --i-mean-it)")
	cmd.Flags().Bool("i-mean-it", false, "Second confirmation flag for overwrite")
	cmd.Flags().Bool("allow-chain-id-mismatch", false, "Persist GenesisState.allow_chain_id_mismatch=true so InitGenesis skips the soft chain_human_name vs chain_id check (§11.1); use only for legitimate name/chain-id divergence")
	return cmd
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the chain identity currently in genesis.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)
			genFile, err := genesisFilePath(serverCtx)
			if err != nil {
				return err
			}
			appState, _, err := loadAppState(genFile)
			if err != nil {
				return err
			}
			raw, ok := appState[types.ModuleName]
			if !ok {
				return fmt.Errorf("app_state.%s not initialized", types.ModuleName)
			}
			out, _ := json.MarshalIndent(json.RawMessage(raw), "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

func validateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the chain identity in genesis.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			allowMismatch, _ := cmd.Flags().GetBool("allow-chain-id-mismatch")
			serverCtx := server.GetServerContextFromCmd(cmd)
			genFile, err := genesisFilePath(serverCtx)
			if err != nil {
				return err
			}
			appState, doc, err := loadAppState(genFile)
			if err != nil {
				return err
			}
			raw, ok := appState[types.ModuleName]
			if !ok {
				return fmt.Errorf("app_state.%s not initialized", types.ModuleName)
			}
			var gs types.GenesisState
			if err := json.Unmarshal(raw, &gs); err != nil {
				return err
			}
			if err := gs.Identity.Validate(); err != nil {
				return err
			}
			if err := gs.Identity.ValidateAgainstChainID(doc.ChainID, allowMismatch); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err.Error())
				return err
			}
			fmt.Println("OK")
			return nil
		},
	}
	cmd.Flags().Bool("allow-chain-id-mismatch", false, "Skip the soft chain_human_name vs chain_id consistency check")
	return cmd
}

// --- helpers -------------------------------------------------------------

type genesisDoc struct {
	ChainID  string          `json:"chain_id"`
	AppState json.RawMessage `json:"app_state"`
	Rest     map[string]json.RawMessage
}

func genesisFilePath(serverCtx *server.Context) (string, error) {
	home := serverCtx.Config.RootDir
	if home == "" {
		if v, ok := serverCtx.Viper.Get("home").(string); ok {
			home = v
		} else {
			return "", fmt.Errorf("could not determine node home directory")
		}
	}
	return filepath.Join(home, "config", "genesis.json"), nil
}

func loadAppState(path string) (map[string]json.RawMessage, *genesisDoc, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bz, &raw); err != nil {
		return nil, nil, fmt.Errorf("unmarshal genesis.json: %w", err)
	}
	doc := &genesisDoc{Rest: raw}
	if cid, ok := raw["chain_id"]; ok {
		_ = json.Unmarshal(cid, &doc.ChainID)
	}
	if appStateRaw, ok := raw["app_state"]; ok {
		doc.AppState = appStateRaw
	}
	var appState map[string]json.RawMessage
	if len(doc.AppState) == 0 {
		appState = make(map[string]json.RawMessage)
	} else {
		if err := json.Unmarshal(doc.AppState, &appState); err != nil {
			return nil, nil, fmt.Errorf("unmarshal app_state: %w", err)
		}
	}
	return appState, doc, nil
}

func saveAppState(path string, doc *genesisDoc, appState map[string]json.RawMessage) error {
	newAppState, err := json.Marshal(appState)
	if err != nil {
		return err
	}
	doc.Rest["app_state"] = newAppState
	out, err := json.MarshalIndent(doc.Rest, "", "  ")
	if err != nil {
		return err
	}
	// Preserve the original file mode if it exists; default to 0o600 for new
	// files. genesis.json is precious; the atomic temp-write + rename pattern
	// guarantees we never leave a half-written file even on signal/crash.
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp for atomic write: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic rename: %w", err)
	}
	return nil
}

// wireSDKSentinels installs %BOND_DENOM% in the SDK module params that hold a
// denom literal. The sentinel rewrite (§7.3) substitutes them at chain start.
func wireSDKSentinels(as map[string]json.RawMessage) error {
	if err := rewriteSubpath(as, "staking", "params", "bond_denom", types.BondDenomSentinel); err != nil {
		return err
	}
	if err := rewriteSubpath(as, "mint", "params", "mint_denom", types.BondDenomSentinel); err != nil {
		return err
	}
	if err := rewriteSubpath(as, "crisis", "constant_fee", "denom", types.BondDenomSentinel); err != nil {
		return err
	}
	if err := rewriteCoinSlice(as, "gov", "params", "min_deposit", types.BondDenomSentinel); err != nil {
		return err
	}
	if err := rewriteCoinSlice(as, "gov", "params", "expedited_min_deposit", types.BondDenomSentinel); err != nil {
		return err
	}
	return nil
}

// rewriteSubpath walks a 3-level path in app_state and overwrites the final
// string value. Missing intermediate keys cause a no-op.
func rewriteSubpath(as map[string]json.RawMessage, module, paramsKey, field, value string) error {
	mRaw, ok := as[module]
	if !ok {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(mRaw, &m); err != nil {
		return err
	}
	pRaw, ok := m[paramsKey]
	if !ok {
		return nil
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal(pRaw, &p); err != nil {
		return err
	}
	bz, err := json.Marshal(value)
	if err != nil {
		return err
	}
	p[field] = bz
	pNew, err := json.Marshal(p)
	if err != nil {
		return err
	}
	m[paramsKey] = pNew
	mNew, err := json.Marshal(m)
	if err != nil {
		return err
	}
	as[module] = mNew
	return nil
}

// rewriteCoinSlice walks app_state.<module>.<paramsKey>.<field> (an array of
// sdk.Coin) and replaces every entry's denom with the supplied sentinel,
// preserving amounts.
func rewriteCoinSlice(as map[string]json.RawMessage, module, paramsKey, field, sentinelDenom string) error {
	mRaw, ok := as[module]
	if !ok {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(mRaw, &m); err != nil {
		return err
	}
	pRaw, ok := m[paramsKey]
	if !ok {
		return nil
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal(pRaw, &p); err != nil {
		return err
	}
	fRaw, ok := p[field]
	if !ok {
		return nil
	}
	var coins []sdk.Coin
	if err := json.Unmarshal(fRaw, &coins); err != nil {
		// Try without amino strict typing — params can be loose.
		var loose []map[string]json.RawMessage
		if err2 := json.Unmarshal(fRaw, &loose); err2 != nil {
			return err
		}
		for _, entry := range loose {
			entry["denom"], _ = json.Marshal(sentinelDenom)
		}
		newF, err := json.Marshal(loose)
		if err != nil {
			return err
		}
		p[field] = newF
		pNew, err := json.Marshal(p)
		if err != nil {
			return err
		}
		m[paramsKey] = pNew
		mNew, err := json.Marshal(m)
		if err != nil {
			return err
		}
		as[module] = mNew
		return nil
	}
	for i := range coins {
		coins[i].Denom = sentinelDenom
	}
	newF, err := json.Marshal(coins)
	if err != nil {
		return err
	}
	p[field] = newF
	pNew, err := json.Marshal(p)
	if err != nil {
		return err
	}
	m[paramsKey] = pNew
	mNew, err := json.Marshal(m)
	if err != nil {
		return err
	}
	as[module] = mNew
	return nil
}

var _ servertypes.AppOptions // silence unused import in some build configs

// Marshal helper for client.Context (keeps the import live for tooling that
// later threads it through).
var _ = client.Context{}
