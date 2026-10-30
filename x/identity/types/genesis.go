package types

import "fmt"

// DefaultGenesis returns the chain identity baked into this binary via
// the build-tag-selectable DefaultChainIdentity() (see
// default_identity.go for mainnet, default_identity_testnet.go for the
// `testnet` build tag).
//
// Operators who want to override at genesis-construction time use
// `sparkdreamd genesis identity init --force --i-mean-it ...` after
// `sparkdreamd init`. See spec §15.
//
// `AllowChainIdMismatch` defaults to true on local and test builds so the
// chain-id consistency check (§11.1) doesn't trip on a default genesis that
// doesn't yet have a chain_id set, and to false on network builds, whose
// chain ids match their names (chain_id_check_*.go). Operators who set a proper chain_id
// matching `ChainHumanName` can leave the field as-is; those who use a
// divergent chain_id should leave `allow_chain_id_mismatch=true` or
// override via `genesis identity init --allow-chain-id-mismatch`.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Identity:             DefaultChainIdentity(),
		AllowChainIdMismatch: defaultAllowChainIdMismatch,
	}
}

// Validate rejects an empty identity and validates the rest against the
// strict intrinsic rules. Every module reads its denoms through
// IdentityKeeper.BondDenom, so a chain without an identity cannot run; an
// empty identity used to pass here and then panic deep inside a later
// module's InitGenesis with a message that did not name the cause.
// Cross-checks against chain_id happen later in InitGenesis via
// ChainIdentity.ValidateAgainstChainID.
func (gs GenesisState) Validate() error {
	if gs.Identity.IsEmpty() {
		return fmt.Errorf("%w: the identity genesis section is empty; set it with `sparkdreamd genesis identity init` or use the default", ErrIdentityNotInitialized)
	}
	return gs.Identity.Validate()
}

// IsEmpty reports whether neither native denom is set.
func (id ChainIdentity) IsEmpty() bool {
	return id.BondDenom == "" && id.DreamDenom == ""
}
