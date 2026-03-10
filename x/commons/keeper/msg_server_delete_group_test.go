package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

// TestMsgDeleteGroup covers standard deletion scenarios:
// 1. Unauthorized stranger
// 2. Group not found
// 3. Successful deletion by the direct Parent address
func TestMsgDeleteGroup(t *testing.T) {
	k, ctx, moduleAddr := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)
	_ = moduleAddr

	// 1. Setup Accounts
	parent := sdk.AccAddress("parent_addr_________")
	stranger := sdk.AccAddress("stranger_addr_______")

	// 2. Create Child via native state injection
	childPolicyAddr := keeper.DeriveCouncilAddress(1, "standard").String()

	// 3. Register in x/commons
	groupName := "Disposable DAO"
	maxSpendPerEpoch := math.NewInt(100)
	group := types.Group{
		GroupId:             1,
		PolicyAddress:       childPolicyAddr,
		ParentPolicyAddress: parent.String(), // The Recorded Parent
		FundingWeight:       10,
		MaxSpendPerEpoch:    &maxSpendPerEpoch,
	}
	require.NoError(t, k.Groups.Set(ctx, groupName, group))
	require.NoError(t, k.PolicyToName.Set(ctx, childPolicyAddr, groupName))

	// Add a member
	require.NoError(t, k.AddMember(ctx, groupName, types.Member{
		Address: parent.String(), Weight: "1",
	}))

	// Set initial policy version
	require.NoError(t, k.PolicyVersion.Set(ctx, childPolicyAddr, 1))

	// 4. Save Permissions (to verify cleanup)
	err := k.PolicyPermissions.Set(ctx, childPolicyAddr, types.PolicyPermissions{
		PolicyAddress:   childPolicyAddr,
		AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		msg       types.MsgDeleteGroup
		expectErr bool
		errMsg    string
		check     func(t *testing.T)
	}{
		{
			name: "Unauthorized: Stranger tries to delete",
			msg: types.MsgDeleteGroup{
				Authority: stranger.String(),
				GroupName: groupName,
			},
			expectErr: true,
			errMsg:    "unauthorized",
		},
		{
			name: "Failure: Group Not Found",
			msg: types.MsgDeleteGroup{
				Authority: parent.String(),
				GroupName: "Ghost Group",
			},
			expectErr: true,
			errMsg:    "not found",
		},
		{
			name: "Success: Parent deletes Child",
			msg: types.MsgDeleteGroup{
				Authority: parent.String(),
				GroupName: groupName,
			},
			expectErr: false,
			check: func(t *testing.T) {
				// A. Verify Group is removed
				_, err := k.Groups.Get(ctx, groupName)
				require.Error(t, err)

				// B. Verify Permissions are removed
				_, err = k.PolicyPermissions.Get(ctx, childPolicyAddr)
				require.Error(t, err)

				// C. Verify members are cleared
				members, err := k.GetCouncilMembers(ctx, groupName)
				require.NoError(t, err)
				require.Empty(t, members, "All members should be removed")

				// D. Verify policy version was bumped (invalidating pending proposals)
				version, err := k.GetPolicyVersion(ctx, childPolicyAddr)
				require.NoError(t, err)
				require.Greater(t, version, uint64(1), "Policy version should be bumped")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ms.DeleteGroup(ctx, &tc.msg)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
				if tc.check != nil {
					tc.check(t)
				}
			}
		})
	}
}

// TestMsgDeleteGroup_SiblingVeto verifies:
// A Veto Policy (Address B) can delete a group owned by Standard Policy (Address A)
// IF both are registered as policies of the same parent council.
func TestMsgDeleteGroup_SiblingVeto(t *testing.T) {
	k, ctx, _ := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup "The Council" (Grandparent Group) with standard and veto policies
	parentAddr := keeper.DeriveCouncilAddress(1, "standard").String()
	vetoAddr := keeper.DeriveCouncilAddress(1, "veto").String()

	// Register the council
	councilGroup := types.Group{
		GroupId:       1,
		PolicyAddress: parentAddr,
		MinMembers:    1,
		MaxMembers:    10,
	}
	require.NoError(t, k.Groups.Set(ctx, "The Council", councilGroup))
	require.NoError(t, k.PolicyToName.Set(ctx, parentAddr, "The Council"))
	require.NoError(t, k.PolicyToName.Set(ctx, vetoAddr, "The Council"))
	require.NoError(t, k.VetoPolicies.Set(ctx, "The Council", vetoAddr))

	// 2. Setup "The Committee" (Child Group to be deleted)
	childAddr := keeper.DeriveCouncilAddress(2, "standard").String()

	groupName := "Rogue DAO"
	group := types.Group{
		GroupId:             2,
		PolicyAddress:       childAddr,
		ParentPolicyAddress: parentAddr, // <--- Standard Policy is the recorded Parent
		FundingWeight:       0,
	}
	require.NoError(t, k.Groups.Set(ctx, groupName, group))
	require.NoError(t, k.PolicyToName.Set(ctx, childAddr, groupName))

	// Set initial policy version
	require.NoError(t, k.PolicyVersion.Set(ctx, childAddr, 1))

	// 3. Execute DeleteGroup signed by VETO Policy
	// This proves that "Sibling Check" works.
	msg := &types.MsgDeleteGroup{
		Authority: vetoAddr, // <--- Signed by Veto Policy (Different Address!)
		GroupName: groupName,
	}

	_, err := ms.DeleteGroup(ctx, msg)
	require.NoError(t, err)

	// 4. Verify Deletion
	_, err = k.Groups.Get(ctx, groupName)
	require.Error(t, err)
}

func TestMsgDeleteGroup_ByGov(t *testing.T) {
	k, ctx, _ := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// Addresses
	parent := sdk.AccAddress("parent_addr_________")
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()

	// 1. Create via native state
	policyAddr := keeper.DeriveCouncilAddress(1, "standard").String()

	// 2. Register in Commons
	groupName := "Rogue DAO"
	group := types.Group{
		GroupId:             1,
		PolicyAddress:       policyAddr,
		ParentPolicyAddress: parent.String(),
	}
	require.NoError(t, k.Groups.Set(ctx, groupName, group))
	require.NoError(t, k.PolicyToName.Set(ctx, policyAddr, groupName))

	// Set initial policy version
	require.NoError(t, k.PolicyVersion.Set(ctx, policyAddr, 1))

	// Action: Gov deletes the group (bypassing Parent check)
	msg := types.MsgDeleteGroup{
		Authority: govAddr,
		GroupName: groupName,
	}

	_, err := ms.DeleteGroup(ctx, &msg)
	require.NoError(t, err)

	// Verify Removal
	_, err = k.Groups.Get(ctx, groupName)
	require.Error(t, err)
}

// TestMsgDeleteGroup_ZombieKill verifies that pending proposals are invalidated
// when a group is deleted via the Policy Version bump mechanism.
func TestMsgDeleteGroup_ZombieKill(t *testing.T) {
	k, ctx, _ := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup Accounts
	admin := sdk.AccAddress("admin_______________") // Parent
	member := sdk.AccAddress("member______________")

	// 2. Create via native state
	policyAddr := keeper.DeriveCouncilAddress(1, "standard").String()

	// 3. Set initial policy version
	require.NoError(t, k.PolicyVersion.Set(ctx, policyAddr, 1))

	// 4. Create a native proposal (to prove it gets killed via version mismatch)
	proposalSeqID, err := k.ProposalSeq.Next(ctx)
	require.NoError(t, err)

	proposal := types.Proposal{
		Id:            proposalSeqID,
		CouncilName:   "Zombie Group",
		PolicyAddress: policyAddr,
		Proposer:      member.String(),
		Status:        types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		SubmitTime:    ctx.BlockTime().Unix(),
		PolicyVersion: 1, // Matches initial version
	}
	require.NoError(t, k.Proposals.Set(ctx, proposalSeqID, proposal))

	// 5. Register group in x/commons
	groupName := "Zombie Group"
	require.NoError(t, k.Groups.Set(ctx, groupName, types.Group{
		GroupId:             1,
		PolicyAddress:       policyAddr,
		ParentPolicyAddress: admin.String(), // Admin acts as Parent
		FundingWeight:       0,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, policyAddr, groupName))

	// Add member
	require.NoError(t, k.AddMember(ctx, groupName, types.Member{
		Address: member.String(), Weight: "1",
	}))

	// 6. Execute Delete Group (The "Poison Pill")
	_, err = ms.DeleteGroup(ctx, &types.MsgDeleteGroup{
		Authority: admin.String(),
		GroupName: groupName,
	})
	require.NoError(t, err)

	// 7. Assertions

	// A. Verify Version Bump (1 -> 2)
	newVersion, err := k.GetPolicyVersion(ctx, policyAddr)
	require.NoError(t, err)
	require.Equal(t, uint64(2), newVersion, "Policy version must increment to invalidate old proposals")

	// B. Verify Zombie Proposal is effectively dead
	// The proposal still exists but its PolicyVersion (1) no longer matches the current version (2)
	savedProp, err := k.Proposals.Get(ctx, proposalSeqID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), savedProp.PolicyVersion)
	require.NotEqual(t, savedProp.PolicyVersion, newVersion, "Proposal version must NOT match current policy version")

	// C. Verify Members are Removed
	members, err := k.GetCouncilMembers(ctx, groupName)
	require.NoError(t, err)
	require.Empty(t, members, "All members should be removed")
}
