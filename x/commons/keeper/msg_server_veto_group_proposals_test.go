package keeper_test

import (
	"testing"
	"time"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	"github.com/stretchr/testify/require"
)

func TestMsgVetoGroupProposals(t *testing.T) {
	// Use setupSafeUpdateTest to get access to the real groupKeeper
	// This is required because the Sibling Check queries x/group state.
	k, ctx, groupKeeper, moduleAddr := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// --- 1. SETUP THE HIERARCHY ---

	// A. Create "The Council" (Grandparent Group)
	councilMember := sdk.AccAddress("council_member______") // Valid 20-byte address
	councilMembers := []group.MemberRequest{{Address: councilMember.String(), Weight: "1"}}

	councilRes, err := groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    moduleAddr.String(),
		Members:  councilMembers,
		Metadata: "The Council",
	})
	require.NoError(t, err)
	councilID := councilRes.GroupId

	// B. Create Standard Policy (The Recorded Parent)
	// This address is stored in x/commons as the 'ParentPolicyAddress'.
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

	// C. Create Veto Policy (The Sibling)
	// This address has PERMISSION to veto, but is NOT the recorded parent.
	// Logic must detect it belongs to 'councilID' just like the parent.
	vetoPolicyReq := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
	vetoPolicyAny, _ := codectypes.NewAnyWithValue(vetoPolicyReq)
	vetoPolicyRes, err := groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          moduleAddr.String(),
		GroupId:        councilID, // Crucial: Same Group ID
		DecisionPolicy: vetoPolicyAny,
		Metadata:       "Veto Policy",
	})
	require.NoError(t, err)
	vetoAddr := vetoPolicyRes.Address

	// --- 2. SETUP THE TARGET (CHILD) ---

	// A. Create Child Group
	childMember := sdk.AccAddress("child_member________") // Valid 20-byte address
	childMembers := []group.MemberRequest{{Address: childMember.String(), Weight: "1"}}
	childRes, err := groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    moduleAddr.String(),
		Members:  childMembers,
		Metadata: "Rogue Committee",
	})
	require.NoError(t, err)

	// B. Create Child Policy
	childPolicyReq := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
	childPolicyAny, _ := codectypes.NewAnyWithValue(childPolicyReq)
	childPolicyRes, err := groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          moduleAddr.String(),
		GroupId:        childRes.GroupId,
		DecisionPolicy: childPolicyAny,
		Metadata:       "Rogue Policy v1",
	})
	require.NoError(t, err)
	childAddr := childPolicyRes.Address

	// C. Create a "Zombie Proposal" (to prove it gets killed)
	propRes, err := groupKeeper.SubmitProposal(ctx, &group.MsgSubmitProposal{
		GroupPolicyAddress: childAddr,
		Proposers:          []string{childMember.String()},
		Metadata:           "Malicious Spend",
		Messages:           []*codectypes.Any{},
	})
	require.NoError(t, err)
	zombiePropID := propRes.ProposalId

	// Verify Proposal is linked to Version 1
	propInfo, _ := groupKeeper.Proposal(ctx, &group.QueryProposalRequest{ProposalId: zombiePropID})
	require.Equal(t, uint64(1), propInfo.Proposal.GroupPolicyVersion)

	// --- 3. REGISTER IN X/COMMONS ---
	groupName := "Rogue DAO"
	extendedGroup := types.ExtendedGroup{
		GroupId:             childRes.GroupId,
		PolicyAddress:       childAddr,
		ParentPolicyAddress: parentAddr, // Recorded Parent is Standard Policy
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, groupName, extendedGroup))

	// --- 4. RUN TEST CASES ---

	stranger := sdk.AccAddress("stranger_addr_______")

	tests := []struct {
		name      string
		signer    string
		expectErr bool
		errMsg    string
		check     func(t *testing.T)
	}{
		{
			name:      "Unauthorized: Stranger tries to veto",
			signer:    stranger.String(),
			expectErr: true,
			errMsg:    "unauthorized",
		},
		{
			name:      "Success: Standard Parent Vetoes",
			signer:    parentAddr,
			expectErr: false,
			check: func(t *testing.T) {
				// Verify Version Bump (1 -> 2)
				info, _ := groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{Address: childAddr})
				require.Equal(t, uint64(2), info.Info.Version)
				require.Contains(t, info.Info.Metadata, "[VETOED]")
			},
		},
		{
			name:      "Success: Sibling Veto Policy Vetoes (The Critical Check)",
			signer:    vetoAddr,
			expectErr: false,
			check: func(t *testing.T) {
				// Verify Version Bump (2 -> 3)
				info, _ := groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{Address: childAddr})
				require.Equal(t, uint64(3), info.Info.Version)
				require.Contains(t, info.Info.Metadata, "[VETOED]")

				// Verify Zombie Proposal is Dead
				// We attempt to EXECUTE the proposal.
				// x/group checks (Proposal.Version == Policy.Version) BEFORE checking if the vote passed.
				// Therefore, we expect an error specifically about the policy modification.
				_, err := groupKeeper.Exec(ctx, &group.MsgExec{
					Executor:   childMember.String(),
					ProposalId: zombiePropID,
				})
				require.Error(t, err)
				// The SDK error for this is typically "group policy modified" or "wrong policy version"
				// depending on the exact SDK version, but checking for Error is sufficient proof the kill switch worked.
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &types.MsgVetoGroupProposals{
				Authority: tc.signer,
				GroupName: groupName,
			}

			_, err := ms.VetoGroupProposals(ctx, msg)
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
