package app

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/stretchr/testify/require"

	commonskeeper "sparkdream/x/commons/keeper"
	commonstypes "sparkdream/x/commons/types"
)

// Treasury staking against the real x/staking, x/distribution and x/gov
// (x/commons/keeper/treasury_staking.go, gov_tally.go): a committee
// delegates treasury SPARK through the commons wrapper, the stake cap and
// the member-validator rule hold, the stake has no x/gov voting power, and
// deleting the committee unbonds the stake and sweeps it to the parent
// council.
func TestTreasuryStakingEndToEnd(t *testing.T) {
	// The default genesis bootstraps the councils around one account, which
	// also operates the chain's only validator: no e2e snapshot is needed.
	app, ctx := newAppFromDefaultGenesis(t)
	const groupName = "Commons Operations Committee"
	group, err := app.CommonsKeeper.Groups.Get(ctx, groupName)
	require.NoError(t, err)
	policy := group.PolicyAddress
	policyAddr := sdk.MustAccAddressFromBech32(policy)
	parentAddr := sdk.MustAccAddressFromBech32(group.ParentPolicyAddress)
	ms := commonskeeper.NewMsgServerImpl(app.CommonsKeeper)

	denom, err := app.StakingKeeper.BondDenom(ctx)
	require.NoError(t, err)
	vals, err := app.StakingKeeper.GetBondedValidatorsByPower(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, vals)
	val := vals[0]
	valAddr, err := sdk.ValAddressFromBech32(val.GetOperator())
	require.NoError(t, err)
	operator := sdk.AccAddress(valAddr)

	_, bonded, headroom, err := app.CommonsKeeper.TreasuryStake(ctx)
	require.NoError(t, err)
	require.True(t, headroom.IsPositive(), "bonded %s leaves no headroom", bonded)

	// Fund the treasury beyond the headroom so only the cap can refuse.
	fund := sdk.NewCoins(sdk.NewCoin(denom, headroom.MulRaw(4)))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, fund))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, policyAddr, fund))

	stake := sdk.NewCoin(denom, headroom.QuoRaw(2))
	require.True(t, stake.Amount.IsPositive())
	delegate := func(amount sdk.Coin) error {
		_, err := ms.DelegateTreasury(ctx, &commonstypes.MsgDelegateTreasury{
			Authority: policy, ValidatorAddress: val.GetOperator(), Amount: amount,
		})
		return err
	}

	// A committee may not delegate to its own member's validator. The
	// founder runs the only validator and sits on the committee; taking the
	// seat away lets the rest of the test use that validator.
	isMember, err := app.CommonsKeeper.HasMember(ctx, groupName, operator.String())
	require.NoError(t, err)
	require.True(t, isMember, "the founder sits on the committee")
	require.ErrorIs(t, delegate(stake), commonstypes.ErrValidatorOperatedByMember)
	require.NoError(t, app.CommonsKeeper.RemoveMember(ctx, groupName, operator.String()))
	// The rule also covers the groups above the committee: the founder
	// sits on its parent council too.
	parentName, _, found := app.CommonsKeeper.GetGroupByPolicy(ctx, group.ParentPolicyAddress)
	require.True(t, found)
	require.ErrorIs(t, delegate(stake), commonstypes.ErrValidatorOperatedByMember)
	require.NoError(t, app.CommonsKeeper.RemoveMember(ctx, parentName, operator.String()))

	require.ErrorIs(t, delegate(sdk.NewCoin(denom, headroom.AddRaw(1))), commonstypes.ErrTreasuryStakeCap)
	require.NoError(t, delegate(stake))
	del, err := app.StakingKeeper.GetDelegation(ctx, policyAddr, valAddr)
	require.NoError(t, err)
	val, err = app.StakingKeeper.GetValidator(ctx, valAddr)
	require.NoError(t, err)
	treasuryTokens := val.TokensFromShares(del.Shares)
	require.Equal(t, stake.Amount, treasuryTokens.TruncateInt())

	staked, _, _, err := app.CommonsKeeper.TreasuryStake(ctx)
	require.NoError(t, err)
	require.Equal(t, stake.Amount, staked)

	// x/gov: the validator votes yes and carries its delegators, the
	// treasury excepted, whether or not the treasury votes itself.
	tallyYes := func(treasuryVotes bool) sdkmath.Int {
		cacheCtx, _ := ctx.CacheContext()
		proposal, err := app.GovKeeper.SubmitProposal(cacheCtx, nil, "", "treasury stake", "has no voting power", operator, false)
		require.NoError(t, err)
		require.NoError(t, app.GovKeeper.ActivateVotingPeriod(cacheCtx, proposal))
		require.NoError(t, app.GovKeeper.AddVote(cacheCtx, proposal.Id, operator, govv1.NewNonSplitVoteOption(govv1.OptionYes), ""))
		if treasuryVotes {
			require.NoError(t, app.GovKeeper.AddVote(cacheCtx, proposal.Id, policyAddr, govv1.NewNonSplitVoteOption(govv1.OptionNo), ""))
		}
		proposal, err = app.GovKeeper.Proposals.Get(cacheCtx, proposal.Id)
		require.NoError(t, err)
		_, _, tally, err := app.GovKeeper.Tally(cacheCtx, proposal)
		require.NoError(t, err)
		require.Equal(t, "0", tally.NoCount)
		yes, ok := sdkmath.NewIntFromString(tally.YesCount)
		require.True(t, ok)
		return yes
	}
	want := sdkmath.LegacyNewDecFromInt(val.BondedTokens()).Sub(treasuryTokens).TruncateInt()
	require.Equal(t, want, tallyYes(false))
	require.Equal(t, want, tallyYes(true))

	// Deleting the committee queues the stake; the EndBlocker unbonds it and
	// sweeps the matured SPARK to the parent council.
	_, err = ms.DeleteGroup(ctx, &commonstypes.MsgDeleteGroup{Authority: group.ParentPolicyAddress, GroupName: groupName})
	require.NoError(t, err)
	addrs, err := app.CommonsKeeper.TreasuryAddresses(ctx)
	require.NoError(t, err)
	require.Contains(t, addrs, policy)

	require.NoError(t, app.CommonsKeeper.EndBlockTreasuryWindDowns(ctx))
	_, err = app.StakingKeeper.GetDelegation(ctx, policyAddr, valAddr)
	require.Error(t, err, "the delegation should be unbonding")
	ubds, err := app.StakingKeeper.GetUnbondingDelegations(ctx, policyAddr, 10)
	require.NoError(t, err)
	require.Len(t, ubds, 1)

	parentBefore := app.BankKeeper.GetBalance(ctx, parentAddr, denom)
	unbondingTime, err := app.StakingKeeper.UnbondingTime(ctx)
	require.NoError(t, err)
	later := ctx.WithBlockTime(ctx.BlockTime().Add(unbondingTime + time.Hour)).WithBlockHeight(ctx.BlockHeight() + 1)
	_, err = app.StakingKeeper.BlockValidatorUpdates(later)
	require.NoError(t, err)
	require.NoError(t, app.CommonsKeeper.EndBlockTreasuryWindDowns(later))

	parentAfter := app.BankKeeper.GetBalance(later, parentAddr, denom)
	require.True(t, parentAfter.Amount.Sub(parentBefore.Amount).GTE(stake.Amount),
		"parent received %s, want at least %s", parentAfter.Amount.Sub(parentBefore.Amount), stake.Amount)
	require.True(t, app.BankKeeper.GetBalance(later, policyAddr, denom).IsZero())
	addrs, err = app.CommonsKeeper.TreasuryAddresses(later)
	require.NoError(t, err)
	require.NotContains(t, addrs, policy)
}
