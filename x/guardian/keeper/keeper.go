// Package keeper implements the guardian module: a generic authority-gating
// proxy that owns the authority address for one or more downstream modules
// (bank, mint, staking, distribution, gov, slashing, auth, consensus) and
// routes gov-submitted msgs through per-msg-type field filters before
// dispatch.
//
// Why this exists: a stock cosmos chain configures sensitive module
// authorities as the gov module. That gives gov universal power to alter
// any module param via MsgUpdateParams (and friends). For sovereign-chain
// concerns where specific fields (mint inflation, bond/mint denom, native
// bank send-enabled, community_tax, slashing fractions, voting period,
// block max gas, tx-gas costs) must remain immutable or bounded, guardian
// acts as a proxy:
//
//  1. At genesis, target modules' Authority is set to guardian's module
//     address (instead of gov's).
//  2. Gov can no longer invoke MsgUpdateParams etc. directly on those
//     modules; the authority check fails.
//  3. Gov instead submits MsgExec to guardian with the inner msg packed as
//     Any. Guardian's handler verifies the inner msg type is allowlisted,
//     applies the per-msg-type filter, substitutes guardian's address as
//     the inner Authority, and routes via the msg service router.
//  4. Immutable / out-of-bounds fields are rejected at the filter layer.
//     Mutable ones pass through to the target handler.
//
// See docs/x-identity-spec.md §14.6 and the implementation-decisions doc.
package keeper

import (
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/guardian/types"
)

// Keeper is the guardian keeper.
type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	msgRouter    types.MessageRouter

	// authority is the address allowed to invoke MsgExec (gov in production
	// wiring).
	authority string

	// Late-bound from app.go via Set*Keeper. Filters that depend on these
	// either pass through (for params-bearing filters) or reject (for
	// identity-bearing filters) until set.
	identityKeeper types.IdentityKeeper
	mintKeeper     types.MintKeeper
	stakingKeeper  types.StakingKeeper
	distrKeeper    types.DistrKeeper
	govKeeper      types.GovKeeper
	slashingKeeper types.SlashingKeeper
	authKeeper     types.AuthKeeper
}

// NewKeeper constructs the guardian keeper. The downstream keepers are nil
// at this point; app.go calls Set*Keeper post-depinject.
func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	msgRouter types.MessageRouter,
	authority string,
) Keeper {
	return Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		msgRouter:    msgRouter,
		authority:    authority,
	}
}

// SetIdentityKeeper late-binds the identity keeper used by the
// bank.MsgSetSendEnabled filter to recognize the chain's native denoms.
// Write-once.
func (k *Keeper) SetIdentityKeeper(idk types.IdentityKeeper) {
	if k.identityKeeper != nil {
		panic("guardian: SetIdentityKeeper called twice")
	}
	k.identityKeeper = idk
}

// SetMintKeeper late-binds the mint keeper used by the mint.MsgUpdateParams
// filter to compare proposed inflation params to current. Write-once.
func (k *Keeper) SetMintKeeper(mk types.MintKeeper) {
	if k.mintKeeper != nil {
		panic("guardian: SetMintKeeper called twice")
	}
	k.mintKeeper = mk
}

// SetStakingKeeper late-binds the staking keeper used by the
// staking.MsgUpdateParams filter to compare proposed bond_denom to current.
// Write-once.
func (k *Keeper) SetStakingKeeper(sk types.StakingKeeper) {
	if k.stakingKeeper != nil {
		panic("guardian: SetStakingKeeper called twice")
	}
	k.stakingKeeper = sk
}

// SetDistrKeeper late-binds the distribution keeper used by the
// distribution.MsgUpdateParams filter. Write-once.
func (k *Keeper) SetDistrKeeper(dk types.DistrKeeper) {
	if k.distrKeeper != nil {
		panic("guardian: SetDistrKeeper called twice")
	}
	k.distrKeeper = dk
}

// SetGovKeeper late-binds the gov keeper used by the gov.MsgUpdateParams
// filter. Write-once.
func (k *Keeper) SetGovKeeper(gk types.GovKeeper) {
	if k.govKeeper != nil {
		panic("guardian: SetGovKeeper called twice")
	}
	k.govKeeper = gk
}

// SetSlashingKeeper late-binds the slashing keeper used by the
// slashing.MsgUpdateParams filter. Write-once.
func (k *Keeper) SetSlashingKeeper(sk types.SlashingKeeper) {
	if k.slashingKeeper != nil {
		panic("guardian: SetSlashingKeeper called twice")
	}
	k.slashingKeeper = sk
}

// SetAuthKeeper late-binds the auth keeper used by the auth.MsgUpdateParams
// filter. Write-once.
func (k *Keeper) SetAuthKeeper(ak types.AuthKeeper) {
	if k.authKeeper != nil {
		panic("guardian: SetAuthKeeper called twice")
	}
	k.authKeeper = ak
}

// ModuleAddress returns the guardian module's account address. This is the
// address that target modules set as their Authority at genesis.
func ModuleAddress() string {
	return authtypes.NewModuleAddress(types.ModuleName).String()
}

// authorityCheck returns nil if the supplied address matches the configured
// gov authority.
func (k Keeper) authorityCheck(authority string) error {
	if authority != k.authority {
		return errorsmod.Wrapf(types.ErrUnauthorized, "expected %q, got %q", k.authority, authority)
	}
	return nil
}

// AllowedMsgTypes returns the sorted list of inner msg type URLs the
// guardian will route. Built into the binary; gov cannot extend it at
// runtime (a chain upgrade is required to add or remove entries).
//
// Includes distribution.MsgCommunityPoolSpend even though guardian's filter
// rejects it: the listing makes "what powers does gov have over the
// community pool?" auditable in one place (x/split is the canonical path,
// any direct spend is denied), instead of being invisibly absent.
//
// Note: cosmos-sdk v0.53.6 does not expose bank.MsgSetDenomMetadata as a
// gov-routable msg, so native-denom Symbol/Display protection is handled
// by the bank-keeper wrapper (app/bank_guard.go) and identity invariant 4,
// not by a guardian filter.
func AllowedMsgTypes() []string {
	return []string{
		"/cosmos.auth.v1beta1.MsgUpdateParams",
		"/cosmos.bank.v1beta1.MsgSetSendEnabled",
		"/cosmos.bank.v1beta1.MsgUpdateParams",
		"/cosmos.consensus.v1.MsgUpdateParams",
		"/cosmos.distribution.v1beta1.MsgCommunityPoolSpend",
		"/cosmos.distribution.v1beta1.MsgUpdateParams",
		"/cosmos.gov.v1.MsgUpdateParams",
		"/cosmos.mint.v1beta1.MsgUpdateParams",
		"/cosmos.slashing.v1beta1.MsgUpdateParams",
		"/cosmos.staking.v1beta1.MsgUpdateParams",
	}
}
