package keeper_test

import (
	"sparkdream/x/commons/types"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func TestCycleDetectionLogic(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// SCENARIO: Create a ring: A -> B -> C -> A

	// 1. Setup Addresses
	addrA := sdk.AccAddress([]byte("policy_A____________")).String()
	addrB := sdk.AccAddress([]byte("policy_B____________")).String()
	addrC := sdk.AccAddress([]byte("policy_C____________")).String()

	// 2. Manually Stitch the Graph in State (Bypassing MsgServer)
	// Group A (Parent is B)
	require.NoError(t, k.ExtendedGroup.Set(ctx, "GroupA", types.ExtendedGroup{PolicyAddress: addrA, ParentPolicyAddress: addrB}))
	require.NoError(t, k.PolicyToName.Set(ctx, addrA, "GroupA"))

	// Group B (Parent is C)
	require.NoError(t, k.ExtendedGroup.Set(ctx, "GroupB", types.ExtendedGroup{PolicyAddress: addrB, ParentPolicyAddress: addrC}))
	require.NoError(t, k.PolicyToName.Set(ctx, addrB, "GroupB"))

	// Group C (Parent is Gov for now - Open Chain)
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
	require.NoError(t, k.ExtendedGroup.Set(ctx, "GroupC", types.ExtendedGroup{PolicyAddress: addrC, ParentPolicyAddress: govAddr}))
	require.NoError(t, k.PolicyToName.Set(ctx, addrC, "GroupC"))

	// 3. Test 1: No Cycle (Linear)
	// Check if A is in ancestry of C? (C -> Gov). No.
	hasCycle, err := k.DetectCycle(ctx, addrA, addrC)
	require.NoError(t, err)
	require.False(t, hasCycle, "A -> C should be valid (linear)")

	// 4. Test 2: Close the Loop (Cycle)
	// Update C to have Parent = A.
	// Now: C -> A -> B -> C ...
	require.NoError(t, k.ExtendedGroup.Set(ctx, "GroupC", types.ExtendedGroup{PolicyAddress: addrC, ParentPolicyAddress: addrA}))

	// Trigger Check: Is C in ancestry of B?
	// Ancestry of B: B -> C -> A -> B ...
	// Since 'child' is C, and we ask if C is in B's ancestry, it should be YES.
	hasCycle, err = k.DetectCycle(ctx, addrC, addrB)
	require.NoError(t, err)
	require.True(t, hasCycle, "Cycle C->A->B->C should be detected")

	// 5. Test 3: Self Loop
	hasCycle, err = k.DetectCycle(ctx, addrA, addrA)
	require.NoError(t, err)
	require.True(t, hasCycle, "Self-loop should be detected")
}
