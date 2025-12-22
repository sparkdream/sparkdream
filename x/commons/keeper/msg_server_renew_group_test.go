package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func TestRenewGroup(t *testing.T) {
	// Reusing the setup helper from safe_update_parent_test.go
	k, ctx, groupK, commonsModuleAddr := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup Addresses
	parentPolicyAddr := sdk.AccAddress([]byte("parent_policy_______"))
	attackerAddr := sdk.AccAddress([]byte("attacker_address____"))

	// Get Governance Module Address (The Supreme Authority)
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	member1 := sdk.AccAddress([]byte("member1_____________"))
	member2 := sdk.AccAddress([]byte("member2_____________"))
	member3 := sdk.AccAddress([]byte("member3_____________"))
	futarchyBot := sdk.AccAddress([]byte("futarchy_bot________"))

	// 2. Setup CHILD Group
	childRes, err := groupK.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:   commonsModuleAddr.String(),
		Members: []group.MemberRequest{{Address: member1.String(), Weight: "1"}},
	})
	require.NoError(t, err)
	childGroupID := childRes.GroupId

	// 3. Define Timestamps
	now := ctx.BlockTime().Unix()
	expiredTime := now - 100 // Term ended 100s ago
	activeTime := now + 3600 // Term ends in 1 hour

	// 4. Register Extended Groups

	// Case A: Expired Group (Ready for renewal)
	expiredGroup := types.ExtendedGroup{
		GroupId:               childGroupID,
		ParentPolicyAddress:   parentPolicyAddr.String(),
		MinMembers:            2,
		MaxMembers:            5,
		TermDuration:          86400, // 1 Day
		CurrentTermExpiration: expiredTime,
		FutarchyEnabled:       false,
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "ExpiredGroup", expiredGroup))

	// Case B: Active Group
	activeGroup := types.ExtendedGroup{
		GroupId:               childGroupID,
		ParentPolicyAddress:   parentPolicyAddr.String(),
		MinMembers:            1,
		MaxMembers:            5,
		TermDuration:          86400,
		CurrentTermExpiration: activeTime,
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "ActiveGroup", activeGroup))

	// Case C: Futarchy Enabled Group
	futarchyGroup := types.ExtendedGroup{
		GroupId:               childGroupID,
		ParentPolicyAddress:   parentPolicyAddr.String(),
		MinMembers:            1,
		MaxMembers:            10,
		TermDuration:          86400,
		CurrentTermExpiration: expiredTime,
		FutarchyEnabled:       true,
		FutarchyMemberAddress: futarchyBot.String(),
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "FutarchyGroup", futarchyGroup))

	// Case D: Size Check Group (Specific for failure tests)
	// We use this group solely to fail on size constraints.
	// It is expired so we don't hit the time lock check first.
	sizeCheckGroup := types.ExtendedGroup{
		GroupId:               childGroupID,
		ParentPolicyAddress:   parentPolicyAddr.String(),
		MinMembers:            2,
		MaxMembers:            5,
		TermDuration:          86400,
		CurrentTermExpiration: expiredTime,
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "SizeCheckGroup", sizeCheckGroup))

	tests := []struct {
		desc      string
		msg       *types.MsgRenewGroup
		expectErr bool
		errType   error
		check     func(t *testing.T)
	}{
		{
			desc: "Success - Standard Renewal",
			msg: &types.MsgRenewGroup{
				Authority:        parentPolicyAddr.String(),
				GroupName:        "ExpiredGroup",
				NewMembers:       []string{member1.String(), member2.String()},
				NewMemberWeights: []string{"1", "1"},
			},
			expectErr: false,
			check: func(t *testing.T) {
				g, _ := k.ExtendedGroup.Get(ctx, "ExpiredGroup")
				expectedExpiration := ctx.BlockTime().Unix() + g.TermDuration
				require.Equal(t, expectedExpiration, g.CurrentTermExpiration)

				resp, _ := groupK.GroupMembers(ctx, &group.QueryGroupMembersRequest{GroupId: childGroupID})
				require.Len(t, resp.Members, 2)
			},
		},
		{
			desc: "Success - Governance Override (Bypass Term Check & Bypass Min/Max)",
			msg: &types.MsgRenewGroup{
				Authority: govAddr,       // SIGNER IS X/GOV
				GroupName: "ActiveGroup", // Term is NOT expired
				// Only 1 member, but that's fine for Gov override
				NewMembers:       []string{member1.String()},
				NewMemberWeights: []string{"1"},
			},
			expectErr: false,
			check: func(t *testing.T) {
				g, _ := k.ExtendedGroup.Get(ctx, "ActiveGroup")
				require.True(t, g.CurrentTermExpiration > activeTime, "Expiration should have been pushed forward")
			},
		},
		{
			desc: "Success - Futarchy Logic (20% Weight)",
			msg: &types.MsgRenewGroup{
				Authority:  parentPolicyAddr.String(),
				GroupName:  "FutarchyGroup",
				NewMembers: []string{member1.String(), member2.String(), member3.String()},
				// Total Human Weight = 1 + 1 + 2 = 4 -> Futarchy = 1
				NewMemberWeights: []string{"1", "1", "2"},
			},
			expectErr: false,
			check: func(t *testing.T) {
				resp, _ := groupK.GroupMembers(ctx, &group.QueryGroupMembersRequest{GroupId: childGroupID})
				require.Len(t, resp.Members, 4)

				foundBot := false
				for _, m := range resp.Members {
					if m.Member.Address == futarchyBot.String() {
						foundBot = true
						require.Equal(t, "1", m.Member.Weight)
					}
				}
				require.True(t, foundBot, "futarchy bot should be added automatically")
			},
		},
		{
			desc: "Failure - Term Not Expired (Standard User)",
			msg: &types.MsgRenewGroup{
				Authority:        parentPolicyAddr.String(),
				GroupName:        "ActiveGroup",
				NewMembers:       []string{member1.String()},
				NewMemberWeights: []string{"1"},
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest, // "current term has not expired yet"
		},
		{
			desc: "Failure - Unauthorized (Attacker)",
			msg: &types.MsgRenewGroup{
				Authority:        attackerAddr.String(),
				GroupName:        "ExpiredGroup",
				NewMembers:       []string{member1.String(), member2.String()},
				NewMemberWeights: []string{"1", "1"},
			},
			expectErr: true,
			errType:   sdkerrors.ErrUnauthorized,
		},
		{
			desc: "Failure - Below Min Members (Standard User)",
			msg: &types.MsgRenewGroup{
				Authority:        parentPolicyAddr.String(),
				GroupName:        "SizeCheckGroup",           // Min is 2
				NewMembers:       []string{member1.String()}, // Providing 1
				NewMemberWeights: []string{"1"},
			},
			expectErr: true,
			errType:   types.ErrInvalidGroupSize,
		},
		{
			desc: "Failure - Above Max Members (Standard User)",
			msg: &types.MsgRenewGroup{
				Authority: parentPolicyAddr.String(),
				GroupName: "SizeCheckGroup", // Max is 5
				// Providing 6 members
				NewMembers:       []string{member1.String(), member2.String(), member3.String(), attackerAddr.String(), parentPolicyAddr.String(), futarchyBot.String()},
				NewMemberWeights: []string{"1", "1", "1", "1", "1", "1"},
			},
			expectErr: true,
			errType:   types.ErrInvalidGroupSize,
		},
		{
			desc: "Failure - Array Length Mismatch",
			msg: &types.MsgRenewGroup{
				Authority:        parentPolicyAddr.String(),
				GroupName:        "SizeCheckGroup",
				NewMembers:       []string{member1.String()},
				NewMemberWeights: []string{"1", "1"}, // 2 weights for 1 member
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := ms.RenewGroup(ctx, tc.msg)

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
