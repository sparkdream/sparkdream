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
	idRaw, ok := as[types.ModuleName]
	if !ok {
		// V1 DEVIATION FROM SPEC: when identity is absent from genesis, the
		// rewrite hook becomes a no-op so legacy single-chain deployments
		// (with hardcoded `uspark`/`dream` literals throughout) keep working
		// during the migration. Federated deployments MUST provide identity
		// or the chain starts with no sentinel substitution and a broken
		// genesis. See docs/x-identity-implementation-decisions.md.
		return rawAppState, types.ChainIdentity{}, nil
	}
	var idGS types.GenesisState
	if err := cdc.UnmarshalJSON(idRaw, &idGS); err != nil {
		return nil, types.ChainIdentity{}, fmt.Errorf("unmarshal identity genesis: %w", err)
	}
	// Allow an empty identity entry to also act as a no-op (same logic as
	// the missing-key branch above).
	if idGS.Identity.BondDenom == "" && idGS.Identity.DreamDenom == "" {
		return rawAppState, types.ChainIdentity{}, nil
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

// purgeLegacyBankMetadata removes legacy-denom DenomMetadata entries from
// app_state.bank.denom_metadata. Conservative: only the literal set
// {uspark, udream, dream, stake} is removed, and an entry whose Base equals
// the chain's chosen denom is preserved (degenerate test config support).
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
