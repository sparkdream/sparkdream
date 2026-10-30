// Package genesisinit implements the pre-InitGenesis sentinel rewrite (§7.3
// of docs/x-identity-spec.md). The rewrite operates on the raw genesis JSON
// blob before any module's InitGenesis runs, substituting the bond/dream
// sentinels with chain-specific denoms drawn from
// app_state.identity.identity.
//
// This package is deliberately kept narrow: it imports only banktypes and
// the identity types package, has no knowledge of any other module's
// schema, and operates purely as a string replace + bank-metadata cleanup
// over JSON bytes. See §7.4 for why a typed AST walk is not used.
package genesisinit

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"sparkdream/x/identity/types"
)

// legacyDenomLiterals is the canonical list of legacy bond/dream denom
// strings purged from app_state.bank.denom_metadata at chain start. Adding
// to this list requires a spec update.
var legacyDenomLiterals = []string{"uspark", "udream", "dream", "stake"}

// LegacyDenomLiterals returns a copy of the legacy denom literal set (for
// the genesis validate command).
func LegacyDenomLiterals() []string {
	return append([]string(nil), legacyDenomLiterals...)
}

// RewriteSentinels walks the genesis app_state and replaces the bond/dream
// denom sentinels with the values from app_state.identity.identity. It also
// strips legacy bank denom_metadata entries before bank's own InitGenesis
// loads them.
//
// The codec is required to decode the identity GenesisState because
// gogoproto marshals int64 (FoundedAt) as a JSON string, which stdlib
// encoding/json refuses to read back into an int64 field. The app codec
// implements the JSONPB conventions that produced the bytes.
//
// Returns the rewritten app_state AND the resolved ChainIdentity so callers
// can stash it for logging/metrics without re-parsing.
func RewriteSentinels(cdc codec.Codec, rawAppState json.RawMessage) (json.RawMessage, types.ChainIdentity, error) {
	var as map[string]json.RawMessage
	if err := json.Unmarshal(rawAppState, &as); err != nil {
		return nil, types.ChainIdentity{}, fmt.Errorf("unmarshal app_state: %w", err)
	}
	// Identity is mandatory (docs/x-identity-implementation-decisions.md):
	// a genesis without it fails here, at the rewrite boundary, with the
	// actual cause. Without this check the SDK skips identity's InitGenesis
	// for a missing section and the chain panics later inside an unrelated
	// module's first denom read.
	idRaw, ok := as[types.ModuleName]
	if !ok || len(idRaw) == 0 || string(idRaw) == "null" {
		return nil, types.ChainIdentity{}, fmt.Errorf(
			"app_state.%s is missing: every chain must declare its identity (run `sparkdreamd genesis identity init`)", types.ModuleName)
	}
	var idGS types.GenesisState
	if err := cdc.UnmarshalJSON(idRaw, &idGS); err != nil {
		return nil, types.ChainIdentity{}, fmt.Errorf("unmarshal identity genesis: %w", err)
	}
	if idGS.Identity.BondDenom == "" && idGS.Identity.DreamDenom == "" {
		return nil, types.ChainIdentity{}, fmt.Errorf(
			"app_state.%s.identity is empty: every chain must declare its identity (run `sparkdreamd genesis identity init`)", types.ModuleName)
	}
	if err := idGS.Identity.Validate(); err != nil {
		return nil, types.ChainIdentity{}, fmt.Errorf("invalid chain identity (sentinel rewrite rejects early): %w", err)
	}

	// Refuse to rewrite anything inside app_state.genutil. Gen-txs are signed;
	// substituting bytes invalidates the validator signature. Operators must
	// run `genesis identity init` BEFORE `gentx`.
	if genutilRaw, ok := as["genutil"]; ok {
		if strings.Contains(string(genutilRaw), types.BondDenomSentinel) ||
			strings.Contains(string(genutilRaw), types.DreamDenomSentinel) {
			return nil, types.ChainIdentity{}, fmt.Errorf(
				"sentinel found inside app_state.genutil; gentxs must be generated AFTER `genesis identity init` so they sign against the resolved denom")
		}
	}

	s := string(rawAppState)
	s = strings.ReplaceAll(s, types.BondDenomSentinel, idGS.Identity.BondDenom)
	s = strings.ReplaceAll(s, types.DreamDenomSentinel, idGS.Identity.DreamDenom)
	if strings.Contains(s, types.BondDenomSentinel) || strings.Contains(s, types.DreamDenomSentinel) {
		return nil, types.ChainIdentity{}, fmt.Errorf("sentinel survived rewrite; logic bug")
	}

	// Re-parse to operate on bank metadata at the structured level.
	if err := json.Unmarshal([]byte(s), &as); err != nil {
		return nil, types.ChainIdentity{}, fmt.Errorf("re-unmarshal after substitution: %w", err)
	}
	if err := purgeLegacyBankMetadata(as, idGS.Identity); err != nil {
		return nil, types.ChainIdentity{}, err
	}
	out, err := json.Marshal(as)
	if err != nil {
		return nil, types.ChainIdentity{}, fmt.Errorf("re-marshal: %w", err)
	}
	return out, idGS.Identity, nil
}

// purgeLegacyBankMetadata cleans app_state.bank.denom_metadata before bank's
// InitGenesis loads it:
//
//   - legacy-denom entries (the literal set {uspark, udream, dream, stake},
//     unless one is the chain's own denom) are removed;
//   - native-denom entries are removed too, so x/identity's canonical
//     metadata is the only record of the two native denoms. Bank's
//     InitGenesis runs after identity's and writes through its raw keeper,
//     bypassing the identity guard, so a native entry left here would
//     overwrite the seal at block 0 (IDN-06). An entry that is not canonical
//     (a ticker, name or decimals that differ from the identity) is refused
//     rather than dropped: it means the operator's genesis disagrees with
//     its own identity.
func purgeLegacyBankMetadata(as map[string]json.RawMessage, id types.ChainIdentity) error {
	bankRaw, ok := as["bank"]
	if !ok {
		return nil
	}
	var bank map[string]json.RawMessage
	if err := json.Unmarshal(bankRaw, &bank); err != nil {
		return fmt.Errorf("unmarshal bank genesis: %w", err)
	}
	metaRaw, ok := bank["denom_metadata"]
	if !ok || len(metaRaw) == 0 || string(metaRaw) == "null" {
		return nil
	}
	var entries []banktypes.Metadata
	if err := json.Unmarshal(metaRaw, &entries); err != nil {
		return fmt.Errorf("unmarshal denom_metadata: %w", err)
	}
	legacy := make(map[string]struct{}, len(legacyDenomLiterals))
	for _, d := range legacyDenomLiterals {
		legacy[d] = struct{}{}
	}
	delete(legacy, id.BondDenom)
	delete(legacy, id.DreamDenom)

	kept := entries[:0]
	for _, m := range entries {
		if _, isLegacy := legacy[m.Base]; isLegacy {
			continue
		}
		if want, native := id.CanonicalMetadataFor(m.Base); native {
			if reason := types.CheckSealedMetadata(m, want); reason != "" {
				return fmt.Errorf("app_state.bank.denom_metadata entry for native denom %q disagrees with the chain identity (%s); remove it: x/identity writes the canonical metadata", m.Base, reason)
			}
			continue
		}
		kept = append(kept, m)
	}
	newMeta, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("re-marshal denom_metadata: %w", err)
	}
	bank["denom_metadata"] = newMeta
	newBank, err := json.Marshal(bank)
	if err != nil {
		return fmt.Errorf("re-marshal bank genesis: %w", err)
	}
	as["bank"] = newBank
	return nil
}
