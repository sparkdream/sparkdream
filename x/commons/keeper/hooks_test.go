package keeper_test

import (
	"testing"

	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
)

// Baseline term duration for hook tests: 10 days worth of seconds.
const hookTermDuration int64 = 10 * 24 * 60 * 60

func TestAfterMarketResolved_UnlinkedMarket_Ignored(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// Market 1 is never linked. Hook should silently succeed.
	require.NoError(t, k.AfterMarketResolved(ctx, 1, "yes"))
}

func TestAfterMarketResolved_Yes_ExtendsTerm(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	groupName := "TenuredCouncil"
	initialExpiration := ctx.BlockTime().Unix() + hookTermDuration
	require.NoError(t, k.Groups.Set(ctx, groupName, types.Group{
		Index:                 groupName,
		TermDuration:          hookTermDuration,
		CurrentTermExpiration: initialExpiration,
	}))
	require.NoError(t, k.MarketToGroup.Set(ctx, 42, groupName))

	require.NoError(t, k.AfterMarketResolved(ctx, 42, "yes"))

	got, err := k.Groups.Get(ctx, groupName)
	require.NoError(t, err)
	require.Equal(t, initialExpiration+hookTermDuration/5, got.CurrentTermExpiration)

	// Market link cleaned up.
	_, err = k.MarketToGroup.Get(ctx, 42)
	require.Error(t, err)
}

func TestAfterMarketResolved_Yes_CappedAtTwoTerms(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	groupName := "AlreadyExtended"
	// Term already far in the future so the +20% bonus would exceed the cap.
	initialExpiration := ctx.BlockTime().Unix() + hookTermDuration*5
	require.NoError(t, k.Groups.Set(ctx, groupName, types.Group{
		TermDuration:          hookTermDuration,
		CurrentTermExpiration: initialExpiration,
	}))
	require.NoError(t, k.MarketToGroup.Set(ctx, 1, groupName))

	require.NoError(t, k.AfterMarketResolved(ctx, 1, "yes"))

	got, err := k.Groups.Get(ctx, groupName)
	require.NoError(t, err)
	maxExpiration := ctx.BlockTime().Unix() + hookTermDuration*2
	require.Equal(t, maxExpiration, got.CurrentTermExpiration)
}

func TestAfterMarketResolved_No_ShortensTerm(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	groupName := "NoConfidenceCouncil"
	initialExpiration := ctx.BlockTime().Unix() + hookTermDuration
	require.NoError(t, k.Groups.Set(ctx, groupName, types.Group{
		TermDuration:          hookTermDuration,
		CurrentTermExpiration: initialExpiration,
	}))
	require.NoError(t, k.MarketToGroup.Set(ctx, 7, groupName))

	require.NoError(t, k.AfterMarketResolved(ctx, 7, "no"))

	got, err := k.Groups.Get(ctx, groupName)
	require.NoError(t, err)
	require.Equal(t, initialExpiration-hookTermDuration/2, got.CurrentTermExpiration)
}

func TestAfterMarketResolved_No_ClampedToNow(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	groupName := "AboutToExpire"
	// Term ends very soon, penalty would push it into the past.
	initialExpiration := ctx.BlockTime().Unix() + 10
	require.NoError(t, k.Groups.Set(ctx, groupName, types.Group{
		TermDuration:          hookTermDuration,
		CurrentTermExpiration: initialExpiration,
	}))
	require.NoError(t, k.MarketToGroup.Set(ctx, 2, groupName))

	require.NoError(t, k.AfterMarketResolved(ctx, 2, "no"))

	got, err := k.Groups.Get(ctx, groupName)
	require.NoError(t, err)
	require.Equal(t, ctx.BlockTime().Unix(), got.CurrentTermExpiration)
}

func TestAfterMarketResolved_InvalidWinner_NoOp(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	groupName := "NoQuorumCouncil"
	initialExpiration := ctx.BlockTime().Unix() + hookTermDuration
	require.NoError(t, k.Groups.Set(ctx, groupName, types.Group{
		TermDuration:          hookTermDuration,
		CurrentTermExpiration: initialExpiration,
	}))
	require.NoError(t, k.MarketToGroup.Set(ctx, 3, groupName))

	require.NoError(t, k.AfterMarketResolved(ctx, 3, "invalid"))

	// Term untouched.
	got, err := k.Groups.Get(ctx, groupName)
	require.NoError(t, err)
	require.Equal(t, initialExpiration, got.CurrentTermExpiration)

	// Link is preserved on invalid winners (hook returns early without removal).
	linked, err := k.MarketToGroup.Get(ctx, 3)
	require.NoError(t, err)
	require.Equal(t, groupName, linked)
}

func TestAfterMarketResolved_MissingGroup_Errors(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// Market points at a group that was never created.
	require.NoError(t, k.MarketToGroup.Set(ctx, 99, "PhantomCouncil"))

	err := k.AfterMarketResolved(ctx, 99, "yes")
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-existent group")
}
