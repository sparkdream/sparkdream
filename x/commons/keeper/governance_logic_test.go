package keeper_test

import (
	"testing"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"
)

func TestScheduleNextMarket(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	const termDuration int64 = 3600
	groupName := "SchedCouncil"

	require.NoError(t, k.ScheduleNextMarket(ctx, groupName, termDuration))

	expectedTrigger := ctx.BlockTime().Unix() + termDuration/2
	has, err := k.MarketTriggerQueue.Has(ctx, collections.Join(expectedTrigger, groupName))
	require.NoError(t, err)
	require.True(t, has)
}

func TestScheduleNextMarket_MultipleCouncils(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	require.NoError(t, k.ScheduleNextMarket(ctx, "A", 100))
	require.NoError(t, k.ScheduleNextMarket(ctx, "B", 200))

	nowA := ctx.BlockTime().Unix() + 50
	nowB := ctx.BlockTime().Unix() + 100

	hasA, err := k.MarketTriggerQueue.Has(ctx, collections.Join(nowA, "A"))
	require.NoError(t, err)
	require.True(t, hasA)

	hasB, err := k.MarketTriggerQueue.Has(ctx, collections.Join(nowB, "B"))
	require.NoError(t, err)
	require.True(t, hasB)
}

func TestTriggerGovernanceMarket_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	const termDuration int64 = 3600
	groupName := "TriggerCouncil"
	require.NoError(t, k.Groups.Set(ctx, groupName, types.Group{
		Index:        groupName,
		TermDuration: termDuration,
	}))

	require.NoError(t, k.TriggerGovernanceMarket(ctx, groupName))

	// mockFutarchyKeeper returns market ID 0.
	linkedGroup, err := k.MarketToGroup.Get(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, groupName, linkedGroup)

	// Next market scheduled at now + term/2.
	expectedTrigger := ctx.BlockTime().Unix() + termDuration/2
	has, err := k.MarketTriggerQueue.Has(ctx, collections.Join(expectedTrigger, groupName))
	require.NoError(t, err)
	require.True(t, has)
}

func TestTriggerGovernanceMarket_MissingGroup(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	err := k.TriggerGovernanceMarket(ctx, "nonexistent_group")
	require.Error(t, err)
}
