package keeper_test

import (
	"fmt"
	"testing"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestResolveDispute(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	// Addresses
	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	claimant := claimantAddr.String()
	ownerAddr := sdk.AccAddress([]byte("owner_address_______"))
	owner := ownerAddr.String()

	authorityAddr, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	name := "alice"

	tests := []struct {
		desc       string
		msg        *types.MsgResolveDispute
		setup      func()
		expectPass bool
		expectErr  string
		check      func(t *testing.T, ctx sdk.Context)
	}{
		{
			desc: "Failure - Unauthorized sender",
			msg: &types.MsgResolveDispute{
				Authority:        claimant, // Not authorized
				Name:             name,
				TransferApproved: true,
			},
			setup: func() {
				f.mockRep.Reset()
				f.mockCommons.Reset()
			},
			expectPass: false,
			expectErr:  "unauthorized",
		},
		{
			desc: "Failure - Dispute not found",
			msg: &types.MsgResolveDispute{
				Authority:        authorityAddr,
				Name:             "nonexistent",
				TransferApproved: true,
			},
			setup: func() {
				f.mockRep.Reset()
				f.mockCommons.Reset()
			},
			expectPass: false,
			expectErr:  "dispute not found",
		},
		{
			desc: "Success - Uncontested, transfer approved (claimant wins)",
			msg: &types.MsgResolveDispute{
				Authority:        authorityAddr,
				Name:             name,
				TransferApproved: true,
			},
			setup: func() {
				f.mockRep.Reset()
				f.mockCommons.Reset()
				// Setup name owned by someone
				f.keeper.SetName(f.ctx, types.NameRecord{Name: name, Owner: owner})
				f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, name))
				// Create active uncontested dispute
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        name,
					Claimant:    claimant,
					FiledAt:     100,
					StakeAmount: math.NewInt(50),
					Active:      true,
				})
				// Create dispute stake record
				challengeID := fmt.Sprintf("name_dispute:%s:%d", name, 100)
				f.keeper.DisputeStakes.Set(f.ctx, challengeID, types.DisputeStake{
					ChallengeId: challengeID,
					Staker:      claimant,
					Amount:      math.NewInt(50),
				})
			},
			expectPass: true,
			check: func(t *testing.T, ctx sdk.Context) {
				// Claimant's stake should be unlocked (returned)
				unlocked := f.mockRep.UnlockedDREAM[claimant]
				require.True(t, unlocked.Equal(math.NewInt(50)), "Claimant stake should be unlocked")

				// Name should be transferred to claimant
				record, found := f.keeper.GetName(ctx, name)
				require.True(t, found)
				require.Equal(t, claimant, record.Owner)

				// Dispute should be inactive
				dispute, found := f.keeper.GetDispute(ctx, name)
				require.True(t, found)
				require.False(t, dispute.Active)
			},
		},
		{
			desc: "Success - Uncontested, transfer denied (dismiss dispute)",
			msg: &types.MsgResolveDispute{
				Authority:        authorityAddr,
				Name:             "bob",
				TransferApproved: false,
			},
			setup: func() {
				f.mockRep.Reset()
				f.mockCommons.Reset()
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "bob", Owner: owner})
				f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, "bob"))
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "bob",
					Claimant:    claimant,
					FiledAt:     100,
					StakeAmount: math.NewInt(50),
					Active:      true,
				})
				challengeID := fmt.Sprintf("name_dispute:%s:%d", "bob", 100)
				f.keeper.DisputeStakes.Set(f.ctx, challengeID, types.DisputeStake{
					ChallengeId: challengeID,
					Staker:      claimant,
					Amount:      math.NewInt(50),
				})
			},
			expectPass: true,
			check: func(t *testing.T, ctx sdk.Context) {
				// Claimant's stake should be burned (dismissed)
				burned := f.mockRep.BurnedDREAM[claimant]
				require.True(t, burned.Equal(math.NewInt(50)), "Claimant stake should be burned")

				// Name stays with owner
				record, found := f.keeper.GetName(ctx, "bob")
				require.True(t, found)
				require.Equal(t, owner, record.Owner)
			},
		},
		{
			desc: "Success - Contested, transfer approved (claimant wins jury)",
			msg: &types.MsgResolveDispute{
				Authority:        authorityAddr,
				Name:             "charlie",
				TransferApproved: true,
			},
			setup: func() {
				f.mockRep.Reset()
				f.mockCommons.Reset()
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "charlie", Owner: owner})
				f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, "charlie"))
				contestChallengeID := "name_contest:charlie:200"
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:               "charlie",
					Claimant:           claimant,
					FiledAt:            100,
					StakeAmount:        math.NewInt(50),
					Active:             true,
					ContestChallengeId: contestChallengeID,
					ContestedAt:        200,
				})
				// Create both stake records
				challengeID := fmt.Sprintf("name_dispute:%s:%d", "charlie", 100)
				f.keeper.DisputeStakes.Set(f.ctx, challengeID, types.DisputeStake{
					ChallengeId: challengeID,
					Staker:      claimant,
					Amount:      math.NewInt(50),
				})
				f.keeper.ContestStakes.Set(f.ctx, contestChallengeID, types.ContestStake{
					ChallengeId: contestChallengeID,
					Owner:       owner,
					Amount:      math.NewInt(100),
				})
			},
			expectPass: true,
			check: func(t *testing.T, ctx sdk.Context) {
				// Claimant wins: claimant stake unlocked, owner stake burned
				unlocked := f.mockRep.UnlockedDREAM[claimant]
				require.True(t, unlocked.Equal(math.NewInt(50)), "Claimant stake should be unlocked")
				burned := f.mockRep.BurnedDREAM[owner]
				require.True(t, burned.Equal(math.NewInt(100)), "Owner contest stake should be burned")

				// Name transferred to claimant
				record, found := f.keeper.GetName(ctx, "charlie")
				require.True(t, found)
				require.Equal(t, claimant, record.Owner)
			},
		},
		{
			desc: "Success - Contested, transfer denied (owner wins jury)",
			msg: &types.MsgResolveDispute{
				Authority:        authorityAddr,
				Name:             "dave",
				TransferApproved: false,
			},
			setup: func() {
				f.mockRep.Reset()
				f.mockCommons.Reset()
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "dave", Owner: owner})
				f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, "dave"))
				contestChallengeID := "name_contest:dave:200"
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:               "dave",
					Claimant:           claimant,
					FiledAt:            100,
					StakeAmount:        math.NewInt(50),
					Active:             true,
					ContestChallengeId: contestChallengeID,
					ContestedAt:        200,
				})
				challengeID := fmt.Sprintf("name_dispute:%s:%d", "dave", 100)
				f.keeper.DisputeStakes.Set(f.ctx, challengeID, types.DisputeStake{
					ChallengeId: challengeID,
					Staker:      claimant,
					Amount:      math.NewInt(50),
				})
				f.keeper.ContestStakes.Set(f.ctx, contestChallengeID, types.ContestStake{
					ChallengeId: contestChallengeID,
					Owner:       owner,
					Amount:      math.NewInt(100),
				})
			},
			expectPass: true,
			check: func(t *testing.T, ctx sdk.Context) {
				// Owner wins: owner stake unlocked, claimant stake burned
				unlocked := f.mockRep.UnlockedDREAM[owner]
				require.True(t, unlocked.Equal(math.NewInt(100)), "Owner stake should be unlocked")
				burned := f.mockRep.BurnedDREAM[claimant]
				require.True(t, burned.Equal(math.NewInt(50)), "Claimant stake should be burned")

				// Name stays with owner
				record, found := f.keeper.GetName(ctx, "dave")
				require.True(t, found)
				require.Equal(t, owner, record.Owner)
			},
		},
		{
			desc: "Success - Council policy authorized",
			msg: &types.MsgResolveDispute{
				Authority:        f.councilAddr,
				Name:             "eve",
				TransferApproved: true,
			},
			setup: func() {
				f.mockRep.Reset()
				f.mockCommons.Reset()
				f.mockCommons.ExtendedGroups["Commons Council"] = commonstypes.ExtendedGroup{
					PolicyAddress: f.councilAddr,
				}
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "eve", Owner: owner})
				f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, "eve"))
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "eve",
					Claimant:    claimant,
					FiledAt:     100,
					StakeAmount: math.NewInt(50),
					Active:      true,
				})
				challengeID := fmt.Sprintf("name_dispute:%s:%d", "eve", 100)
				f.keeper.DisputeStakes.Set(f.ctx, challengeID, types.DisputeStake{
					ChallengeId: challengeID,
					Staker:      claimant,
					Amount:      math.NewInt(50),
				})
			},
			expectPass: true,
		},
		{
			desc: "Failure - Dispute not active",
			msg: &types.MsgResolveDispute{
				Authority:        authorityAddr,
				Name:             "frank",
				TransferApproved: true,
			},
			setup: func() {
				f.mockRep.Reset()
				f.mockCommons.Reset()
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "frank",
					Claimant:    claimant,
					Active:      false,
					StakeAmount: math.NewInt(50),
				})
			},
			expectPass: false,
			expectErr:  "not active",
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			tc.setup()
			cacheCtx, _ := f.ctx.CacheContext()

			_, err := ms.ResolveDispute(cacheCtx, tc.msg)

			if tc.expectPass {
				require.NoError(t, err)
				if tc.check != nil {
					tc.check(t, cacheCtx)
				}
			} else {
				require.Error(t, err)
				if tc.expectErr != "" {
					require.Contains(t, err.Error(), tc.expectErr)
				}
			}
		})
	}
}

func TestResolveDispute_UnauthorizedSenders(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	randomAddr := sdk.AccAddress([]byte("random_address______")).String()

	f.keeper.SetDispute(f.ctx, types.Dispute{
		Name:        "test",
		Claimant:    "claimant",
		Active:      true,
		StakeAmount: math.NewInt(50),
	})

	_, err := ms.ResolveDispute(f.ctx, &types.MsgResolveDispute{
		Authority:        randomAddr,
		Name:             "test",
		TransferApproved: true,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}
