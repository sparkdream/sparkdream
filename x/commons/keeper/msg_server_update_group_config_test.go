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

// Define constants used in the keeper file for validation
const (
	PolicyTypePercentage = "percentage"
	PolicyTypeThreshold  = "threshold"
)

// 2. Define Initial State Template Factory
func createInitialGroupTemplate(ctx sdk.Context, parentPolicy string) types.Group {
	maxSpendPerEpoch := math.NewInt(1000)
	return types.Group{
		GroupId:               1,                                                       // Will be overwritten
		PolicyAddress:         sdk.AccAddress([]byte("child_policy_addr___")).String(), // Will be overwritten
		ParentPolicyAddress:   parentPolicy,
		MaxSpendPerEpoch:      &maxSpendPerEpoch,
		UpdateCooldown:        86400, // 1 Day
		FutarchyEnabled:       true,  // Initial state is true
		MinMembers:            3,
		MaxMembers:            10,
		TermDuration:          100000,
		CurrentTermExpiration: ctx.BlockTime().Unix() + 5000, // Expires in 5000s
	}
}

func TestUpdateGroupConfig(t *testing.T) {
	k, ctx, _ := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup Addresses
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
	parentPolicy := sdk.AccAddress([]byte("parent_policy_______"))
	childGroup := "TechnicalGroup"
	attacker := sdk.AccAddress([]byte("attacker_address____"))

	// Counter for generating unique council addresses
	nextCouncilID := uint64(1)

	// Helper to reset state for tests that modify the group or rely on a fresh start
	resetState := func(t *testing.T) {
		// Get a fresh copy of the template for this run
		template := createInitialGroupTemplate(ctx, parentPolicy.String())

		// Generate a deterministic policy address
		policyAddr := keeper.DeriveCouncilAddress(nextCouncilID, "standard").String()
		nextCouncilID++

		// Update template with generated data
		template.PolicyAddress = policyAddr
		template.GroupId = nextCouncilID

		// Set the custom Group object in the Commons module's store
		require.NoError(t, k.Groups.Set(ctx, childGroup, template))
		require.NoError(t, k.PolicyToName.Set(ctx, policyAddr, childGroup))

		// Add a member
		member1Addr := sdk.AccAddress([]byte("group_member_1______")).String()
		require.NoError(t, k.AddMember(ctx, childGroup, types.Member{
			Address: member1Addr, Weight: "1", Metadata: "m1",
		}))

		// Set initial policy version
		require.NoError(t, k.PolicyVersion.Set(ctx, policyAddr, 1))
	}

	// Establish the base state before running the tests.
	resetState(t)

	// Get a reference to a clean template for assertions
	initialGroupTemplateRef := createInitialGroupTemplate(ctx, parentPolicy.String())

	// Helper to create the local types.BoolValue wrapper pointer for explicit tests
	boolPtrWrapper := func(val bool) *types.BoolValue { return &types.BoolValue{Value: val} }

	maxSpendPerEpoch := math.NewInt(5000)
	negativeSpend := math.NewInt(-100)

	tests := []struct {
		desc        string
		msg         *types.MsgUpdateGroupConfig
		expectErr   bool
		errContains string
		preCheck    func(t *testing.T) // Optional state reset/setup before test run
		checkState  func(t *testing.T)
	}{
		{
			desc:     "Success - Parent Updates Spend Limit (Simple Field, Futarchy Unchanged)",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:        parentPolicy.String(),
				GroupName:        childGroup,
				MaxSpendPerEpoch: &maxSpendPerEpoch,
				// FutarchyEnabled is OMITTED (nil), should remain true
			},
			expectErr: false,
			checkState: func(t *testing.T) {
				g, _ := k.Groups.Get(ctx, childGroup)
				require.Equal(t, math.NewInt(5000), *g.MaxSpendPerEpoch)
				require.Equal(t, initialGroupTemplateRef.MinMembers, g.MinMembers)
				require.True(t, g.FutarchyEnabled) // Must remain true
			},
		},
		{
			desc:     "Success - Gov Updates Member Bounds and Cooldown",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:      govAddr,
				GroupName:      childGroup,
				MinMembers:     5,
				MaxMembers:     15,
				UpdateCooldown: 3600,
				// FutarchyEnabled is OMITTED (nil), should remain true
			},
			expectErr: false,
			checkState: func(t *testing.T) {
				g, _ := k.Groups.Get(ctx, childGroup)
				require.Equal(t, uint64(5), g.MinMembers) // New state for sequential checks
				require.Equal(t, uint64(15), g.MaxMembers)
				require.Equal(t, int64(3600), g.UpdateCooldown)
				require.True(t, g.FutarchyEnabled) // Must remain true
			},
		},
		{
			desc:     "Success - Update Policy to Percentage",
			preCheck: resetState, // Use original state template
			msg: &types.MsgUpdateGroupConfig{
				Authority:          parentPolicy.String(),
				GroupName:          childGroup,
				VoteThreshold:      testutil.DecPtr("0.67"),
				PolicyType:         PolicyTypePercentage,
				VotingPeriod:       172800, // 2 days
				MinExecutionPeriod: 3600,   // 1 hour
				// FutarchyEnabled is OMITTED (nil), should remain true
			},
			expectErr: false,
			checkState: func(t *testing.T) {
				g, _ := k.Groups.Get(ctx, childGroup)
				require.Equal(t, initialGroupTemplateRef.MaxSpendPerEpoch, g.MaxSpendPerEpoch)
				require.True(t, g.FutarchyEnabled) // Must remain true
			},
		},
		{
			desc:     "Success - Explicitly Disable Futarchy",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:       parentPolicy.String(),
				GroupName:       childGroup,
				FutarchyEnabled: boolPtrWrapper(false), // Explicitly set to false using local wrapper
			},
			expectErr: false,
			checkState: func(t *testing.T) {
				g, _ := k.Groups.Get(ctx, childGroup)
				require.False(t, g.FutarchyEnabled) // Must be false
			},
		},
		{
			desc:     "Success - Update Policy to Threshold",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:          parentPolicy.String(),
				GroupName:          childGroup,
				VoteThreshold:      testutil.DecPtr("7"), // Threshold of 7 members
				PolicyType:         PolicyTypeThreshold,
				VotingPeriod:       43200, // 12 hours
				MinExecutionPeriod: 0,     // No min execution time
				// FutarchyEnabled is OMITTED (nil), should remain true
			},
			expectErr: false,
			checkState: func(t *testing.T) {
				g, _ := k.Groups.Get(ctx, childGroup)
				require.Equal(t, initialGroupTemplateRef.TermDuration, g.TermDuration)
				require.True(t, g.FutarchyEnabled) // Must remain true
			},
		},
		{
			desc:     "Failure - Invalid Policy Type",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:          parentPolicy.String(),
				GroupName:          childGroup,
				VoteThreshold:      testutil.DecPtr("1"),
				PolicyType:         "invalid", // Invalid
				VotingPeriod:       86400,
				MinExecutionPeriod: 0,
			},
			expectErr:   true,
			errContains: "invalid policy_type",
		},
		{
			desc:     "Failure - Zero Voting Period",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:          parentPolicy.String(),
				GroupName:          childGroup,
				VoteThreshold:      testutil.DecPtr("1"),
				PolicyType:         PolicyTypeThreshold,
				VotingPeriod:       0, // Invalid
				MinExecutionPeriod: 0,
			},
			expectErr:   true,
			errContains: "voting_period must be greater than 0",
		},
		{
			desc: "Failure - Max Members < Existing Min (Partial Update)",
			preCheck: func(t *testing.T) {
				// 1. Reset state to establish Group in store
				resetState(t)

				// 2. Manually update the MinMembers in the Group store
				g_new, _ := k.Groups.Get(ctx, childGroup)
				g_new.MinMembers = 5
				require.NoError(t, k.Groups.Set(ctx, childGroup, g_new))
			},
			msg: &types.MsgUpdateGroupConfig{
				Authority:  parentPolicy.String(),
				GroupName:  childGroup,
				MaxMembers: 4, // Min is 5 in state for this test
			},
			expectErr:   true,
			errContains: "max_members (4) cannot be less than min_members (5)",
		},
		{
			desc:     "Failure - Unauthorized User",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority: attacker.String(),
				GroupName: childGroup,
			},
			expectErr:   true,
			errContains: sdkerrors.ErrUnauthorized.Error(),
		},
		{
			desc:     "Failure - Invalid Coin Format",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:        parentPolicy.String(),
				GroupName:        childGroup,
				MaxSpendPerEpoch: &negativeSpend,
			},
			expectErr:   true,
			errContains: "cannot be negative",
		},
		{
			desc:     "Failure - Max Members < Min Members (Direct Update)",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:  parentPolicy.String(),
				GroupName:  childGroup,
				MinMembers: 20,
				MaxMembers: 10,
			},
			expectErr:   true,
			errContains: "max_members (10) cannot be less than min_members (20)",
		},
		{
			desc:     "Safety Check - Term Duration Update does NOT extend current term",
			preCheck: resetState,
			msg: &types.MsgUpdateGroupConfig{
				Authority:    parentPolicy.String(),
				GroupName:    childGroup,
				TermDuration: 999999, // Massive extension
			},
			expectErr: false,
			checkState: func(t *testing.T) {
				g, _ := k.Groups.Get(ctx, childGroup)
				// The Rule changed:
				require.Equal(t, int64(999999), g.TermDuration)
				// The Deadline did NOT change (Safety Mechanism):
				require.Equal(t, initialGroupTemplateRef.CurrentTermExpiration, g.CurrentTermExpiration)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			// Pre-check for specific state setup (like resetting state)
			if tc.preCheck != nil {
				tc.preCheck(t)
			}

			_, err := ms.UpdateGroupConfig(ctx, tc.msg)

			if tc.expectErr {
				require.Error(t, err)
				if tc.errContains != "" {
					require.Contains(t, err.Error(), tc.errContains)
				}
			} else {
				require.NoError(t, err)
				if tc.checkState != nil {
					tc.checkState(t)
				}
			}
		})
	}
}
