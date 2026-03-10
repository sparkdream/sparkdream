package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func TestUpdateGroupMembers(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup Addresses
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
	newMember := sdk.AccAddress([]byte("new_member__________"))

	// Helper to create a council with native state injection
	nextCouncilID := uint64(1)
	createCouncil := func(name string) string {
		councilID := nextCouncilID
		nextCouncilID++
		policyAddr := keeper.DeriveCouncilAddress(councilID, "standard").String()
		return policyAddr
	}

	// 2. Register Groups with Hierarchy & Delegation (Using native state)

	// A. Create council addresses
	councilAddr := createCouncil("ParentCouncil")
	electionsAddr := createCouncil("ElectionsCommittee")
	childAddr := createCouncil("RandomChild")

	// B. Elections Committee (The Authorized Manager)
	electionsGroup := types.Group{
		GroupId:             2,
		PolicyAddress:       electionsAddr,
		ParentPolicyAddress: councilAddr,
		UpdateCooldown:      3600, // 1 Hour Cooldown
		LastParentUpdate:    0,
		MinMembers:          1,
		MaxMembers:          10,
	}
	require.NoError(t, k.Groups.Set(ctx, "ElectionsCommittee", electionsGroup))
	require.NoError(t, k.PolicyToName.Set(ctx, electionsAddr, "ElectionsCommittee"))

	// C. Parent Council (delegating to Elections)
	council := types.Group{
		GroupId:                1,
		PolicyAddress:          councilAddr,
		ElectoralPolicyAddress: electionsAddr, // <--- DELEGATION
		MinMembers:             1,
		MaxMembers:             10,
	}
	require.NoError(t, k.Groups.Set(ctx, "ParentCouncil", council))
	require.NoError(t, k.PolicyToName.Set(ctx, councilAddr, "ParentCouncil"))

	// Add initial member to ParentCouncil
	require.NoError(t, k.AddMember(ctx, "ParentCouncil", types.Member{
		Address: govAddr, Weight: "1",
	}))

	// D. Random Child (Unauthorized)
	childGroup := types.Group{
		GroupId:             3,
		PolicyAddress:       childAddr,
		ParentPolicyAddress: councilAddr,
		MinMembers:          1,
		MaxMembers:          10,
	}
	require.NoError(t, k.Groups.Set(ctx, "RandomChild", childGroup))
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
				signer, err := k.Groups.Get(ctx, "ElectionsCommittee")
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
