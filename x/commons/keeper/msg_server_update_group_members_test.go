package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func TestUpdateGroupMembers(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup Addresses
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
	newMember := sdk.AccAddress([]byte("new_member__________"))

	// Helper to create a real x/group Group & Policy
	createRealGroup := func(name string) (uint64, string) {
		// Create Group
		gRes, err := k.GetGroupKeeper().CreateGroup(ctx, &group.MsgCreateGroup{
			Admin:    k.GetModuleAddress().String(),
			Members:  []group.MemberRequest{{Address: govAddr, Weight: "1"}}, // Initial dummy member
			Metadata: name,
		})
		require.NoError(t, err)

		// Create Policy
		policy := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
		policyAny, err := codectypes.NewAnyWithValue(policy)
		require.NoError(t, err)

		pRes, err := k.GetGroupKeeper().CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
			Admin:          k.GetModuleAddress().String(),
			GroupId:        gRes.GroupId,
			DecisionPolicy: policyAny,
		})
		require.NoError(t, err)

		return gRes.GroupId, pRes.Address
	}

	// 2. Register Groups with Hierarchy & Delegation (Using Real IDs/Addresses)

	// A. Parent Council
	councilID, councilAddr := createRealGroup("ParentCouncil")
	// Note: We need the Elections ID/Address before we can save the Parent's delegation,
	// so we create Elections first.

	// B. Elections Committee (The Authorized Manager)
	electionsID, electionsAddr := createRealGroup("ElectionsCommittee")

	electionsGroup := types.ExtendedGroup{
		GroupId:             electionsID,
		PolicyAddress:       electionsAddr,
		ParentPolicyAddress: councilAddr,
		UpdateCooldown:      3600, // 1 Hour Cooldown
		LastParentUpdate:    0,
		MinMembers:          1,
		MaxMembers:          10,
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "ElectionsCommittee", electionsGroup))
	require.NoError(t, k.PolicyToName.Set(ctx, electionsAddr, "ElectionsCommittee"))

	// Now Save Parent (delegating to Elections)
	council := types.ExtendedGroup{
		GroupId:                councilID,
		PolicyAddress:          councilAddr,
		ElectoralPolicyAddress: electionsAddr, // <--- DELEGATION
		MinMembers:             1,
		MaxMembers:             10,
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "ParentCouncil", council))
	require.NoError(t, k.PolicyToName.Set(ctx, councilAddr, "ParentCouncil"))

	// C. Random Child (Unauthorized)
	childID, childAddr := createRealGroup("RandomChild")

	childGroup := types.ExtendedGroup{
		GroupId:             childID,
		PolicyAddress:       childAddr,
		ParentPolicyAddress: councilAddr,
		MinMembers:          1,
		MaxMembers:          10,
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "RandomChild", childGroup))
	require.NoError(t, k.PolicyToName.Set(ctx, childAddr, "RandomChild"))

	// 3. Test Cases
	tests := []struct {
		desc      string
		msg       *types.MsgUpdateGroupMembers
		blockTime int64 // Optional time jump
		expectErr bool
		errType   error
		check     func(t *testing.T)
	}{
		{
			desc: "Success - Gov Updates Council (Root Authority)",
			msg: &types.MsgUpdateGroupMembers{
				Authority:          govAddr,
				GroupPolicyAddress: councilAddr,
				MembersToAdd:       []string{newMember.String()},
				WeightsToAdd:       []string{"1"},
			},
			expectErr: false,
		},
		{
			desc: "Success - Parent Updates Child (Standard Hierarchy)",
			msg: &types.MsgUpdateGroupMembers{
				Authority:          councilAddr,
				GroupPolicyAddress: childAddr,
				MembersToAdd:       []string{newMember.String()},
				WeightsToAdd:       []string{"1"},
			},
			expectErr: false,
		},
		{
			desc: "Success - Designated Electoral Authority Updates Parent",
			msg: &types.MsgUpdateGroupMembers{
				Authority:          electionsAddr, // Signer is ElectionsCommittee
				GroupPolicyAddress: councilAddr,   // Target is ParentCouncil
				MembersToAdd:       []string{newMember.String()},
				WeightsToAdd:       []string{"1"},
			},
			expectErr: false,
			check: func(t *testing.T) {
				// Verify Cooldown was triggered on the SIGNER (ElectionsCommittee)
				signer, err := k.ExtendedGroup.Get(ctx, "ElectionsCommittee")
				require.NoError(t, err)
				require.Equal(t, ctx.BlockTime().Unix(), signer.LastParentUpdate)
			},
		},
		{
			desc: "Failure - Rate Limit (Electoral Authority too fast)",
			msg: &types.MsgUpdateGroupMembers{
				Authority:          electionsAddr,
				GroupPolicyAddress: councilAddr,
				MembersToAdd:       []string{newMember.String()},
				WeightsToAdd:       []string{"1"},
			},
			// Time is same as previous test, so cooldown (3600s) is active
			expectErr: true,
			errType:   types.ErrRateLimitExceeded,
		},
		{
			desc: "Success - Rate Limit Expired (Time Travel)",
			msg: &types.MsgUpdateGroupMembers{
				Authority:          electionsAddr,
				GroupPolicyAddress: councilAddr,
				MembersToAdd:       []string{newMember.String()},
				WeightsToAdd:       []string{"1"},
			},
			blockTime: ctx.BlockTime().Unix() + 4000, // Move past 3600s cooldown
			expectErr: false,
		},
		{
			desc: "Failure - Unauthorized Child (The 'Broad Mutiny' Fix)",
			msg: &types.MsgUpdateGroupMembers{
				Authority:          childAddr,   // Signer is RandomChild
				GroupPolicyAddress: councilAddr, // Target is ParentCouncil
				MembersToAdd:       []string{newMember.String()},
				WeightsToAdd:       []string{"1"},
			},
			// Should fail because RandomChild is NOT the ElectoralPolicyAddress
			expectErr: true,
			errType:   sdkerrors.ErrUnauthorized,
		},
		{
			desc: "Failure - Random User",
			msg: &types.MsgUpdateGroupMembers{
				Authority:          newMember.String(), // Random user
				GroupPolicyAddress: councilAddr,
				MembersToAdd:       []string{newMember.String()},
				WeightsToAdd:       []string{"1"},
			},
			expectErr: true,
			errType:   sdkerrors.ErrUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			// Handle Time Travel
			if tc.blockTime != 0 {
				ctx = ctx.WithBlockTime(time.Unix(tc.blockTime, 0))
			}

			_, err := ms.UpdateGroupMembers(ctx, tc.msg)

			if tc.expectErr {
				require.Error(t, err)
				if tc.errType != nil {
					require.ErrorIs(t, err, tc.errType)
				}
			} else {
				require.NoError(t, err)
				if tc.check != nil {
					tc.check(t)
				}
			}
		})
	}
}
