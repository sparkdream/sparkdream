package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// FUT-101: toggling futarchy off and back on leaves exactly one queued
// confidence-market trigger for the group, so it never runs two loops.
func TestFUT101_FutarchyToggleKeepsOneMarketLoop(t *testing.T) {
	k, ctx, _ := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)
	gov := k.GetModuleAddressByName(govtypes.ModuleName).String()
	const group = "AuroraGroup"
	template := createInitialGroupTemplate(ctx, sdk.AccAddress([]byte("parent_policy_______")).String())
	template.PolicyAddress = keeper.DeriveCouncilAddress(77, "standard").String()
	require.NoError(t, k.Groups.Set(ctx, group, template))
	require.NoError(t, k.PolicyToName.Set(ctx, template.PolicyAddress, group))
	require.NoError(t, k.PolicyVersion.Set(ctx, template.PolicyAddress, 1))

	// The running loop's next trigger, half a term out; another group's
	// entry must be left alone.
	now := ctx.BlockTime().Unix()
	require.NoError(t, k.MarketTriggerQueue.Set(ctx, collections.Join(now+50000, group)))
	require.NoError(t, k.MarketTriggerQueue.Set(ctx, collections.Join(now+10, "OtherGroup")))

	toggle := func(on bool) {
		_, err := ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{
			Authority: gov, GroupName: group, FutarchyEnabled: &types.BoolValue{Value: on},
		})
		require.NoError(t, err)
	}
	entries := func() map[string]int {
		out := map[string]int{}
		require.NoError(t, k.MarketTriggerQueue.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
			out[key.K2()]++
			return false, nil
		}))
		return out
	}

	toggle(false)
	require.Equal(t, 0, entries()[group], "turning futarchy off drops the queued trigger")
	toggle(true)
	toggle(false)
	toggle(true)
	got := entries()
	require.Equal(t, 1, got[group], "one loop after off/on toggles")
	require.Equal(t, 1, got["OtherGroup"])
	has, err := k.MarketTriggerQueue.Has(ctx, collections.Join(now, group))
	require.NoError(t, err)
	require.True(t, has, "re-enabling schedules the first market now")
}
