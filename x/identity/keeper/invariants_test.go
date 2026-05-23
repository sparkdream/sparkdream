package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/identity/keeper"
	"sparkdream/x/identity/types"
)

func toSDKCtx(ctx context.Context) sdk.Context {
	return sdk.UnwrapSDKContext(ctx)
}

func TestInvariantsPassAtFreshGenesis(t *testing.T) {
	f := initFixture(t)
	id := newValidIdentity()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: id, AllowChainIdMismatch: true}))
	ctx := toSDKCtx(f.ctx)

	for _, inv := range []sdk.Invariant{
		keeper.IdentityInitializedInvariant(f.keeper),
		keeper.CanonicalFieldsInvariant(f.keeper),
		keeper.BankMetadataPresentInvariant(f.keeper),
		keeper.BankMetadataCanonicalInvariant(f.keeper),
	} {
		msg, stop := inv(ctx)
		require.False(t, stop, "invariant unexpectedly tripped: %s", msg)
	}
}

func TestInvariantInitializedFailsPreGenesis(t *testing.T) {
	f := initFixture(t)
	ctx := toSDKCtx(f.ctx)
	msg, stop := keeper.IdentityInitializedInvariant(f.keeper)(ctx)
	require.True(t, stop)
	require.NotEmpty(t, msg)
}

// H1 requires bankKeeper at InitGenesis time for non-empty identity. The
// "no-bank" path is therefore unreachable for any chain that initialized
// identity. Test removed as obsolete; the defense-in-depth short-circuit in
// BankMetadataPresentInvariant remains as a guard against
// reconstruction-through-the-pointer.

// mockSK / mockMK implement the trimmed staking/mint interfaces used by
// SDKParamsAlignedInvariant. Signatures match upstream cosmos-sdk
// (context.Context, GetParams returns minttypes.Params).
type mockSK struct{ denom string }

func (m mockSK) BondDenom(_ context.Context) (string, error) { return m.denom, nil }

type mockMK struct{ denom string }

func (m mockMK) GetParams(_ context.Context) (minttypes.Params, error) {
	return minttypes.Params{MintDenom: m.denom}, nil
}

func TestInvariantSDKParamsAlignedAcceptsMatch(t *testing.T) {
	f := initFixture(t)
	id := newValidIdentity()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: id, AllowChainIdMismatch: true}))
	f.keeper.SetStakingKeeper(mockSK{denom: id.BondDenom})
	f.keeper.SetMintKeeper(mockMK{denom: id.BondDenom})
	ctx := toSDKCtx(f.ctx)

	inv := keeper.SDKParamsAlignedInvariant(f.keeper)
	_, stop := inv(ctx)
	require.False(t, stop)
}

func TestInvariantSDKParamsDriftIsWarningGrade(t *testing.T) {
	f := initFixture(t)
	id := newValidIdentity()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: id, AllowChainIdMismatch: true}))
	f.keeper.SetStakingKeeper(mockSK{denom: "stake"})
	f.keeper.SetMintKeeper(mockMK{denom: id.BondDenom})
	ctx := toSDKCtx(f.ctx)

	inv := keeper.SDKParamsAlignedInvariant(f.keeper)
	msg, stop := inv(ctx)
	// Warning-grade: stop must be false, but a message must be present.
	require.False(t, stop)
	require.NotEmpty(t, msg)
}

func TestInvariantSDKParamsNoOpWithoutStakingMint(t *testing.T) {
	// Without staking/mint keepers wired, the invariant returns clean (used
	// by test harnesses that don't bring up the full SDK module stack).
	f := initFixture(t)
	id := newValidIdentity()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: id, AllowChainIdMismatch: true}))
	ctx := toSDKCtx(f.ctx)
	_, stop := keeper.SDKParamsAlignedInvariant(f.keeper)(ctx)
	require.False(t, stop)
}
