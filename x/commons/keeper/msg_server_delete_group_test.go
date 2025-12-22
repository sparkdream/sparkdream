package keeper_test

import (
	"testing"
	"time"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	"github.com/stretchr/testify/require"
)

// TestMsgDeleteGroup covers standard deletion scenarios:
// 1. Unauthorized stranger
// 2. Group not found
// 3. Successful deletion by the direct Parent address
func TestMsgDeleteGroup(t *testing.T) {
	// Use setupSafeUpdateTest to access real groupKeeper (Required for Zombie Kill & Sibling checks)
	k, ctx, groupKeeper, moduleAddr := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup Accounts
	parent := sdk.AccAddress("parent_addr_________")
	stranger := sdk.AccAddress("stranger_addr_______")

	// 2. Create Child Group in x/group (Target to be deleted)
	// We must create it in the real store because MsgDeleteGroup attempts to update its metadata.
	childRes, err := groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    moduleAddr.String(), // Module must be admin to perform the "Zombie Kill"
		Members:  []group.MemberRequest{{Address: parent.String(), Weight: "1"}},
		Metadata: "Disposable Group",
	})
	require.NoError(t, err)

	// Create Child Policy
	policyReq := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
	policyAny, _ := codectypes.NewAnyWithValue(policyReq)
	policyRes, err := groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          moduleAddr.String(),
		GroupId:        childRes.GroupId,
		DecisionPolicy: policyAny,
		Metadata:       "Disposable Policy",
	})
	require.NoError(t, err)
	childPolicyAddr := policyRes.Address

	// 3. Register in x/commons
	groupName := "Disposable DAO"
	extendedGroup := types.ExtendedGroup{
		GroupId:             childRes.GroupId,
		PolicyAddress:       childPolicyAddr,
		ParentPolicyAddress: parent.String(), // The Recorded Parent
		FundingWeight:       10,
		MaxSpendPerEpoch:    "100uspark",
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, groupName, extendedGroup))

	// 4. Save Permissions (to verify cleanup)
	err = k.PolicyPermissions.Set(ctx, childPolicyAddr, types.PolicyPermissions{
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
				// A. Verify ExtendedGroup is removed
				_, err := k.ExtendedGroup.Get(ctx, groupName)
				require.Error(t, err)

				// B. Verify Permissions are removed
				_, err = k.PolicyPermissions.Get(ctx, childPolicyAddr)
				require.Error(t, err)

				// C. Verify x/group Metadata updated ("Zombie Kill")
				info, err := groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{Address: childPolicyAddr})
				require.NoError(t, err)
				require.Equal(t, "DELETED", info.Info.Metadata)
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
// IF both Address A and Address B belong to the same parent Group ID.
func TestMsgDeleteGroup_SiblingVeto(t *testing.T) {
	k, ctx, groupKeeper, moduleAddr := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup "The Council" (Grandparent Group)
	councilMember := sdk.AccAddress("council_member______")
	councilMembers := []group.MemberRequest{{Address: councilMember.String(), Weight: "1"}}

	councilRes, err := groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    moduleAddr.String(),
		Members:  councilMembers,
		Metadata: "The Council",
	})
	require.NoError(t, err)
	councilID := councilRes.GroupId

	// 2. Create Standard Policy (The Recorded Parent)
	// This address will be stored in x/commons as ParentPolicyAddress
	stdPolicyReq := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
	stdPolicyAny, _ := codectypes.NewAnyWithValue(stdPolicyReq)
	stdPolicyRes, err := groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          moduleAddr.String(),
		GroupId:        councilID,
		DecisionPolicy: stdPolicyAny,
		Metadata:       "Standard Policy",
	})
	require.NoError(t, err)
	parentAddr := stdPolicyRes.Address

	// 3. Create Veto Policy (The Sibling/Signer)
	// This address will SIGN the deletion transaction.
	vetoPolicyReq := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
	vetoPolicyAny, _ := codectypes.NewAnyWithValue(vetoPolicyReq)
	vetoPolicyRes, err := groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          moduleAddr.String(),
		GroupId:        councilID, // SAME Group ID as Standard Policy
		DecisionPolicy: vetoPolicyAny,
		Metadata:       "Veto Policy",
	})
	require.NoError(t, err)
	vetoAddr := vetoPolicyRes.Address

	// 4. Setup "The Committee" (Child Group to be deleted)
	childMember := sdk.AccAddress("child_member________")
	childMembers := []group.MemberRequest{{Address: childMember.String(), Weight: "1"}}

	childRes, err := groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    moduleAddr.String(),
		Members:  childMembers,
		Metadata: "Rogue Committee",
	})
	require.NoError(t, err)

	childPolicyReq := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
	childPolicyAny, _ := codectypes.NewAnyWithValue(childPolicyReq)
	childPolicyRes, err := groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          moduleAddr.String(),
		GroupId:        childRes.GroupId,
		DecisionPolicy: childPolicyAny,
		Metadata:       "Rogue Policy",
	})
	require.NoError(t, err)
	childAddr := childPolicyRes.Address

	// 5. Register in x/commons (Parent = Standard Policy)
	groupName := "Rogue DAO"
	extendedGroup := types.ExtendedGroup{
		GroupId:             childRes.GroupId,
		PolicyAddress:       childAddr,
		ParentPolicyAddress: parentAddr, // <--- Standard Policy is the recorded Parent
		FundingWeight:       0,
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, groupName, extendedGroup))

	// 6. Execute DeleteGroup signed by VETO Policy
	// This proves that "Sibling Check" works.
	msg := &types.MsgDeleteGroup{
		Authority: vetoAddr, // <--- Signed by Veto Policy (Different Address!)
		GroupName: groupName,
	}

	_, err = ms.DeleteGroup(ctx, msg)
	require.NoError(t, err)

	// 7. Verify Deletion
	_, err = k.ExtendedGroup.Get(ctx, groupName)
	require.Error(t, err)
}

func TestMsgDeleteGroup_ByGov(t *testing.T) {
	k, ctx, groupKeeper, moduleAddr := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// Addresses
	parent := sdk.AccAddress("parent_addr_________")
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()

	// 1. Create Real Group & Policy
	groupRes, err := groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    moduleAddr.String(),
		Members:  []group.MemberRequest{{Address: parent.String(), Weight: "1"}},
		Metadata: "Rogue Group",
	})
	require.NoError(t, err)

	policyReq := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
	policyAny, _ := codectypes.NewAnyWithValue(policyReq)
	policyRes, err := groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          moduleAddr.String(),
		GroupId:        groupRes.GroupId,
		DecisionPolicy: policyAny,
		Metadata:       "Rogue Policy",
	})
	require.NoError(t, err)

	// 2. Register in Commons
	groupName := "Rogue DAO"
	extendedGroup := types.ExtendedGroup{
		GroupId:             groupRes.GroupId,
		PolicyAddress:       policyRes.Address,
		ParentPolicyAddress: parent.String(),
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, groupName, extendedGroup))

	// Action: Gov deletes the group (bypassing Parent check)
	msg := types.MsgDeleteGroup{
		Authority: govAddr,
		GroupName: groupName,
	}

	_, err = ms.DeleteGroup(ctx, &msg)
	require.NoError(t, err)

	// Verify Removal
	_, err = k.ExtendedGroup.Get(ctx, groupName)
	require.Error(t, err)
}

// TestMsgDeleteGroup_ZombieKill verifies that pending proposals are invalidated
// when a group is deleted via the Policy Version bump mechanism.
func TestMsgDeleteGroup_ZombieKill(t *testing.T) {
	k, ctx, groupKeeper, moduleAddr := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup Accounts
	admin := sdk.AccAddress("admin_______________") // Parent
	member := sdk.AccAddress("member______________")

	// 2. Create a Real x/group Group (Admin = Module)
	groupRes, err := groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    moduleAddr.String(),
		Members:  []group.MemberRequest{{Address: member.String(), Weight: "1"}},
		Metadata: "Zombie Factory",
	})
	require.NoError(t, err)
	groupID := groupRes.GroupId

	// 3. Create a Real x/group Policy
	policyReq := group.NewThresholdDecisionPolicy(
		"1",
		time.Hour, // Voting Period
		0,         // Min Execution Period
	)
	policyAny, _ := codectypes.NewAnyWithValue(policyReq)
	policyRes, err := groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          moduleAddr.String(),
		GroupId:        groupID,
		DecisionPolicy: policyAny,
		Metadata:       "Original Policy",
	})
	require.NoError(t, err)
	policyAddr := policyRes.Address

	// 4. Create a "Zombie" Proposal (Status: SUBMITTED)
	// This proposal captures the Current Policy Version (Version 1)
	propRes, err := groupKeeper.SubmitProposal(ctx, &group.MsgSubmitProposal{
		GroupPolicyAddress: policyAddr,
		Proposers:          []string{member.String()},
		Metadata:           "Malicious Proposal",
		Messages:           []*codectypes.Any{},
	})
	require.NoError(t, err)
	proposalID := propRes.ProposalId

	// Verify Initial State
	initialPolicyInfo, err := groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{Address: policyAddr})
	require.NoError(t, err)
	require.Equal(t, uint64(1), initialPolicyInfo.Info.Version, "Initial policy version should be 1")

	// 5. REGISTER GROUP IN X/COMMONS
	groupName := "Zombie Group"
	require.NoError(t, k.ExtendedGroup.Set(ctx, groupName, types.ExtendedGroup{
		GroupId:             groupID,
		PolicyAddress:       policyAddr,
		ParentPolicyAddress: admin.String(), // Admin acts as Parent
		FundingWeight:       0,
	}))

	// 6. EXECUTE DELETE GROUP (The "Poison Pill")
	_, err = ms.DeleteGroup(ctx, &types.MsgDeleteGroup{
		Authority: admin.String(),
		GroupName: groupName,
	})
	require.NoError(t, err)

	// 7. ASSERTIONS

	// A. Verify Metadata update ("DELETED")
	finalPolicyInfo, err := groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{Address: policyAddr})
	require.NoError(t, err)
	require.Equal(t, "DELETED", finalPolicyInfo.Info.Metadata, "Policy metadata should be updated to DELETED")

	// B. Verify Version Bump (1 -> 2)
	require.Greater(t, finalPolicyInfo.Info.Version, initialPolicyInfo.Info.Version, "Policy version must increment to invalidate old proposals")

	// C. Verify Zombie Proposal is effectively dead
	propInfo, err := groupKeeper.Proposal(ctx, &group.QueryProposalRequest{ProposalId: proposalID})
	require.NoError(t, err)

	// The proposal is stuck on Version 1...
	require.Equal(t, uint64(1), propInfo.Proposal.GroupPolicyVersion)
	// ...but the Policy is now Version 2.
	require.NotEqual(t, propInfo.Proposal.GroupPolicyVersion, finalPolicyInfo.Info.Version, "Proposal version must NOT match current policy version")

	// D. Verify Members are Removed (Weights set to 0)
	membersRes, err := groupKeeper.GroupMembers(ctx, &group.QueryGroupMembersRequest{GroupId: groupID})
	require.NoError(t, err)
	require.Empty(t, membersRes.Members, "All members should be removed (empty list or 0 weight)")
}
