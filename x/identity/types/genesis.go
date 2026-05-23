package types

// DefaultGenesis returns the chain identity baked into this binary via
// the build-tag-selectable DefaultChainIdentity() (see
// default_identity.go for mainnet, default_identity_testnet.go for the
// `testnet` build tag).
//
// Operators who want to override at genesis-construction time use
// `sparkdreamd genesis identity init --force --i-mean-it ...` after
// `sparkdreamd init`. See spec §15.
//
// `AllowChainIdMismatch` defaults to true here so the chain-id
// consistency check (§11.1) doesn't trip on a default genesis that
// doesn't yet have a chain_id set. Operators who set a proper chain_id
// matching `ChainHumanName` can leave the field as-is; those who use a
// divergent chain_id should leave `allow_chain_id_mismatch=true` or
// override via `genesis identity init --allow-chain-id-mismatch`.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Identity:             DefaultChainIdentity(),
		AllowChainIdMismatch: true,
	}
}

// Validate accepts the empty zero-valued identity (single-chain legacy mode)
// and validates non-empty identities against the strict intrinsic rules.
// Cross-checks against chain_id happen later in InitGenesis via
// ChainIdentity.ValidateAgainstChainID.
func (gs GenesisState) Validate() error {
	if gs.Identity.BondDenom == "" && gs.Identity.DreamDenom == "" {
		return nil
	}
	return gs.Identity.Validate()
}
