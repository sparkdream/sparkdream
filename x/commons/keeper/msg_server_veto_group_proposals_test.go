package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgVetoGroupProposals(t *testing.T) {
	k, ctx, _ := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// --- 1. SETUP THE HIERARCHY ---

	// A. Create "The Council" with standard and veto policies
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

	// Add a council member
	councilMember := sdk.AccAddress("council_member______")
	require.NoError(t, k.AddMember(ctx, "The Council", types.Member{
		Address: councilMember.String(), Weight: "1",
	}))

	// --- 2. SETUP THE TARGET (CHILD) ---

	childAddr := keeper.DeriveCouncilAddress(2, "standard").String()
	childMember := sdk.AccAddress("child_member________")

	// Set initial policy version for child
	require.NoError(t, k.PolicyVersion.Set(ctx, childAddr, 1))

	// Create a "Zombie Proposal" (to prove it gets killed via version mismatch)
	proposalSeqID, err := k.ProposalSeq.Next(ctx)
	require.NoError(t, err)

	zombieProposal := types.Proposal{
		Id:            proposalSeqID,
		CouncilName:   "Rogue DAO",
		PolicyAddress: childAddr,
		Proposer:      childMember.String(),
		Status:        types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		SubmitTime:    ctx.BlockTime().Unix(),
		PolicyVersion: 1, // Matches initial version
	}
	require.NoError(t, k.Proposals.Set(ctx, proposalSeqID, zombieProposal))

	// Register child group in x/commons
	groupName := "Rogue DAO"
	group := types.Group{
		GroupId:             2,
		PolicyAddress:       childAddr,
		ParentPolicyAddress: parentAddr, // Recorded Parent is Standard Policy
	}
	require.NoError(t, k.Groups.Set(ctx, groupName, group))
	require.NoError(t, k.PolicyToName.Set(ctx, childAddr, groupName))

	// Add child member
	require.NoError(t, k.AddMember(ctx, groupName, types.Member{
		Address: childMember.String(), Weight: "1",
	}))

	// --- 3. RUN TEST CASES ---

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
				version, err := k.GetPolicyVersion(ctx, childAddr)
				require.NoError(t, err)
				require.Equal(t, uint64(2), version)
			},
		},
		{
			name:      "Success: Sibling Veto Policy Vetoes (The Critical Check)",
			signer:    vetoAddr,
			expectErr: false,
			check: func(t *testing.T) {
				// Verify Version Bump (2 -> 3)
				version, err := k.GetPolicyVersion(ctx, childAddr)
				require.NoError(t, err)
				require.Equal(t, uint64(3), version)

				// Verify Zombie Proposal is Dead
				// The proposal still has PolicyVersion=1 but current version is now 3
				savedProp, err := k.Proposals.Get(ctx, proposalSeqID)
				require.NoError(t, err)
				require.Equal(t, uint64(1), savedProp.PolicyVersion)
				require.NotEqual(t, savedProp.PolicyVersion, version,
					"Proposal version must NOT match current policy version — zombie kill confirmed")
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
