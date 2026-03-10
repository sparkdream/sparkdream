package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"sparkdream/testutil"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func TestRegisterGroup(t *testing.T) {
	// Reuse setup from previous tests
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)

	// 0. Disable Fees for Logic Testing
	// Since we cannot easily fund accounts in this unit test scope without access
	// to the bank keeper internals, we set the fee to empty to bypass the deduction.
	// This ensures we test the hierarchy logic, not the bank balances.
	require.NoError(t, k.Params.Set(ctx, types.NewParams("")))

	// 1. Setup Addresses
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
	attacker := sdk.AccAddress([]byte("attacker____________"))

	// 2. Create a valid parent group to test hierarchy
	parentPolicyAddr := sdk.AccAddress([]byte("existing_policy_____"))
	parentGroup := types.Group{
		PolicyAddress: parentPolicyAddr.String(),
	}
	require.NoError(t, k.Groups.Set(ctx, "ExistingCouncil", parentGroup))
	require.NoError(t, k.PolicyToName.Set(ctx, parentPolicyAddr.String(), "ExistingCouncil"))

	maxSpendPerEpoch := math.NewInt(1000)
	negativeSpend := math.NewInt(-100)

	tests := []struct {
		desc      string
		msg       *types.MsgRegisterGroup
		expectErr bool
		errType   error
		check     func(t *testing.T)
	}{
		{
			desc: "Success - Root (Gov) registers Main Council (Threshold Policy)",
			msg: &types.MsgRegisterGroup{
				Authority:          govAddr,
				Name:               "MainCouncil",
				Description:        "Top level council",
				Members:            []string{sdk.AccAddress([]byte("member1_____________")).String()},
				MemberWeights:      []string{"1"},
				MinMembers:         1,
				MaxMembers:         5,
				TermDuration:       86400,
				VoteThreshold:      testutil.DecPtr("1"),
				PolicyType:         "threshold",
				VotingPeriod:       86400,
				MinExecutionPeriod: 0,
				MaxSpendPerEpoch:   &maxSpendPerEpoch,
				UpdateCooldown:     3600,
				FundingWeight:      50,
			},
			expectErr: false,
			check: func(t *testing.T) {
				// Verify Registry
				g, err := k.Groups.Get(ctx, "MainCouncil")
				require.True(t, err == nil)
				require.Equal(t, govAddr, g.ParentPolicyAddress) // Parent is Gov
				require.Equal(t, uint64(50), g.FundingWeight)

				// Verify Index Creation
				name, err := k.PolicyToName.Get(ctx, g.PolicyAddress)
				require.NoError(t, err)
				require.Equal(t, "MainCouncil", name)
			},
		},
		{
			desc: "Success - Existing Council registers Sub-Committee (Threshold Policy)",
			msg: &types.MsgRegisterGroup{
				Authority:          parentPolicyAddr.String(),
				Name:               "SubCommittee",
				Description:        "Child group",
				Members:            []string{sdk.AccAddress([]byte("member2_____________")).String()},
				MemberWeights:      []string{"1"},
				MinMembers:         1,
				MaxMembers:         3,
				TermDuration:       86400,
				VoteThreshold:      testutil.DecPtr("1"),
				PolicyType:         "threshold",
				VotingPeriod:       86400,
				MinExecutionPeriod: 0,
				FundingWeight:      0,
			},
			expectErr: false,
			check: func(t *testing.T) {
				g, err := k.Groups.Get(ctx, "SubCommittee")
				require.True(t, err == nil)
				require.Equal(t, parentPolicyAddr.String(), g.ParentPolicyAddress) // Linked to Parent

				// Verify Index Creation
				name, err := k.PolicyToName.Get(ctx, g.PolicyAddress)
				require.NoError(t, err)
				require.Equal(t, "SubCommittee", name)
			},
		},
		{
			desc: "Success - Percentage Policy",
			msg: &types.MsgRegisterGroup{
				Authority:     govAddr,
				Name:          "PercentCouncil",
				Description:   "Uses percentage voting",
				Members:       []string{sdk.AccAddress([]byte("member3_____________")).String()},
				MemberWeights: []string{"1"},
				MinMembers:    1, MaxMembers: 5, TermDuration: 86400,
				VoteThreshold:      testutil.DecPtr("0.67"), // Supermajority (e.g.)
				PolicyType:         "percentage",
				VotingPeriod:       86400,
				MinExecutionPeriod: 3600,
				FundingWeight:      10,
			},
			expectErr: false,
			check: func(t *testing.T) {
				g, err := k.Groups.Get(ctx, "PercentCouncil")
				require.True(t, err == nil)
				require.Equal(t, govAddr, g.ParentPolicyAddress)
			},
		},
		{
			desc: "Failure - Unauthorized (Random User / No Profile)",
			msg: &types.MsgRegisterGroup{
				Authority:     attacker.String(),
				Name:          "HackerGroup",
				Members:       []string{attacker.String()},
				MemberWeights: []string{"1"},
				MinMembers:    1, MaxMembers: 5, TermDuration: 100, VoteThreshold: testutil.DecPtr("1"),
				PolicyType:   "threshold",
				VotingPeriod: 86400,
			},
			expectErr: true,
			errType:   sdkerrors.ErrUnauthorized,
		},
		{
			desc: "Failure - Invalid Policy Type",
			msg: &types.MsgRegisterGroup{
				Authority:     govAddr,
				Name:          "BadPolicyType",
				Members:       []string{attacker.String()},
				MemberWeights: []string{"1"},
				MinMembers:    1, MaxMembers: 5, TermDuration: 100, VoteThreshold: testutil.DecPtr("1"),
				PolicyType:         "invalid-type", // Invalid
				VotingPeriod:       86400,
				MinExecutionPeriod: 0,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			desc: "Failure - Zero Voting Period",
			msg: &types.MsgRegisterGroup{
				Authority:     govAddr,
				Name:          "ZeroVotePeriod",
				Members:       []string{attacker.String()},
				MemberWeights: []string{"1"},
				MinMembers:    1, MaxMembers: 5, TermDuration: 100, VoteThreshold: testutil.DecPtr("1"),
				PolicyType:         "threshold",
				VotingPeriod:       0, // Invalid
				MinExecutionPeriod: 0,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			desc: "Failure - Invalid Term Duration",
			msg: &types.MsgRegisterGroup{
				Authority:    govAddr,
				Name:         "BadTermGroup",
				TermDuration: 0, // Invalid
				MinMembers:   1, MaxMembers: 5, VoteThreshold: testutil.DecPtr("1"),
				Members: []string{attacker.String()}, MemberWeights: []string{"1"},
				PolicyType:   "threshold",
				VotingPeriod: 86400,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			desc: "Failure - Min Members > Max Members",
			msg: &types.MsgRegisterGroup{
				Authority:    govAddr,
				Name:         "BadBoundsGroup",
				MinMembers:   10,
				MaxMembers:   5, // Invalid
				TermDuration: 100, VoteThreshold: testutil.DecPtr("1"),
				Members: []string{attacker.String()}, MemberWeights: []string{"1"},
				PolicyType:   "threshold",
				VotingPeriod: 86400,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			desc: "Failure - Initial Members < Min Members",
			msg: &types.MsgRegisterGroup{
				Authority:     govAddr,
				Name:          "UnderstaffedGroup",
				MinMembers:    5, // Requires 5
				MaxMembers:    10,
				Members:       []string{attacker.String()}, // Only providing 1
				MemberWeights: []string{"1"},
				TermDuration:  100, VoteThreshold: testutil.DecPtr("1"),
				PolicyType:   "threshold",
				VotingPeriod: 86400,
			},
			expectErr: true,
			errType:   types.ErrInvalidGroupSize,
		},
		{
			desc: "Failure - Invalid Spend Limit Format",
			msg: &types.MsgRegisterGroup{
				Authority:  govAddr,
				Name:       "BadCoinGroup",
				MinMembers: 1, MaxMembers: 5, TermDuration: 100, VoteThreshold: testutil.DecPtr("1"),
				Members: []string{attacker.String()}, MemberWeights: []string{"1"},
				MaxSpendPerEpoch: &negativeSpend,
				PolicyType:       "threshold",
				VotingPeriod:     86400,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			desc: "Failure - Parent Hijack (User trying to assign Gov as parent)",
			msg: &types.MsgRegisterGroup{
				Authority:             parentPolicyAddr.String(), // Signer is Council
				IntendedParentAddress: govAddr,                   // Tries to assign Gov as parent
				Name:                  "HijackGroup",
				MinMembers:            1, MaxMembers: 5, TermDuration: 100, VoteThreshold: testutil.DecPtr("1"),
				Members: []string{attacker.String()}, MemberWeights: []string{"1"},
				PolicyType:   "threshold",
				VotingPeriod: 86400,
			},
			expectErr: true,
			errType:   sdkerrors.ErrUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := ms.RegisterGroup(ctx, tc.msg)

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
