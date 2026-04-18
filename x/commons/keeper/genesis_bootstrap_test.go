package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
)

func TestSetElectoralDelegation_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	parentName := "ParentCouncil"
	require.NoError(t, k.Groups.Set(ctx, parentName, types.Group{
		Index:         parentName,
		PolicyAddress: "parent_policy",
	}))

	childPolicy := "child_policy_addr"
	require.NoError(t, k.SetElectoralDelegation(ctx, parentName, childPolicy))

	got, err := k.Groups.Get(ctx, parentName)
	require.NoError(t, err)
	require.Equal(t, childPolicy, got.ElectoralPolicyAddress)
}

func TestSetElectoralDelegation_MissingParent(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	err := k.SetElectoralDelegation(ctx, "nonexistent", "child_policy")
	require.Error(t, err)
}

func TestBootstrapConstants(t *testing.T) {
	// Sanity: 5-month term duration matches 5 * 30 days in seconds.
	require.Equal(t, int64(5*30*24*60*60), int64(keeper.TermDuration5Months))
	// 1 year in seconds.
	require.Equal(t, int64(365*24*60*60), int64(keeper.TermDuration1Year))
}
