package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestContestDispute(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	// Addresses
	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	claimant := claimantAddr.String()
	ownerAddr := sdk.AccAddress([]byte("owner_address_______"))
	owner := ownerAddr.String()
	randomAddr := sdk.AccAddress([]byte("random_address______"))
	random := randomAddr.String()

	tests := []struct {
		name       string
		msg        *types.MsgContestDispute
		setup      func()
		expectPass bool
		expectErr  string
		check      func(t *testing.T, ctx sdk.Context)
	}{
		{
			name: "Success - Owner contests dispute",
			msg: &types.MsgContestDispute{
				Authority: owner,
				Name:      "alice",
				Reason:    "I am the rightful owner",
			},
			setup: func() {
				f.mockRep.Reset()
				// Set block height within contest period
				f.ctx = f.ctx.WithBlockHeader(cmtproto.Header{Height: 500})
				// Create name owned by owner
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "alice", Owner: owner})
				// Create active uncontested dispute filed at block 100
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "alice",
					Claimant:    claimant,
					FiledAt:     100,
					StakeAmount: math.NewInt(50),
					Active:      true,
				})
			},
			expectPass: true,
			check: func(t *testing.T, ctx sdk.Context) {
				// Verify DREAM was locked for owner
				locked := f.mockRep.LockedDREAM[owner]
				require.True(t, locked.Equal(types.DefaultContestStakeDream),
					"Expected %s DREAM locked, got %s", types.DefaultContestStakeDream, locked)

				// Verify dispute updated with contest info
				dispute, found := f.keeper.GetDispute(ctx, "alice")
				require.True(t, found)
				require.NotEmpty(t, dispute.ContestChallengeId)
				require.Equal(t, int64(500), dispute.ContestedAt)
				require.True(t, dispute.Active)

				// Verify contest stake record created
				contestStake, err := f.keeper.ContestStakes.Get(ctx, dispute.ContestChallengeId)
				require.NoError(t, err)
				require.Equal(t, owner, contestStake.Owner)
				require.True(t, contestStake.Amount.Equal(types.DefaultContestStakeDream))
			},
		},
		{
			name: "Failure - No dispute found",
			msg: &types.MsgContestDispute{
				Authority: owner,
				Name:      "nonexistent",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
			},
			expectPass: false,
			expectErr:  "dispute not found",
		},
		{
			name: "Failure - Dispute not active",
			msg: &types.MsgContestDispute{
				Authority: owner,
				Name:      "bob",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "bob", Owner: owner})
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "bob",
					Claimant:    claimant,
					Active:      false,
					StakeAmount: math.NewInt(50),
				})
			},
			expectPass: false,
			expectErr:  "not active",
		},
		{
			name: "Failure - Already contested",
			msg: &types.MsgContestDispute{
				Authority: owner,
				Name:      "carol",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "carol", Owner: owner})
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:               "carol",
					Claimant:           claimant,
					Active:             true,
					StakeAmount:        math.NewInt(50),
					ContestChallengeId: "already_contested",
				})
			},
			expectPass: false,
			expectErr:  "already contested",
		},
		{
			name: "Failure - Not the name owner",
			msg: &types.MsgContestDispute{
				Authority: random, // Not the owner
				Name:      "dave",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "dave", Owner: owner})
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "dave",
					Claimant:    claimant,
					FiledAt:     100,
					Active:      true,
					StakeAmount: math.NewInt(50),
				})
			},
			expectPass: false,
			expectErr:  "not the name owner",
		},
		{
			name: "Failure - Contest period expired",
			msg: &types.MsgContestDispute{
				Authority: owner,
				Name:      "eve",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
				// Set block height WAY past the deadline
				f.ctx = f.ctx.WithBlockHeader(cmtproto.Header{Height: 200000})
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "eve", Owner: owner})
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "eve",
					Claimant:    claimant,
					FiledAt:     100, // deadline = 100 + 100800 = 100900, current = 200000
					Active:      true,
					StakeAmount: math.NewInt(50),
				})
			},
			expectPass: false,
			expectErr:  "contest period has expired",
		},
		{
			name: "Failure - DREAM lock fails",
			msg: &types.MsgContestDispute{
				Authority: owner,
				Name:      "frank",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
				f.ctx = f.ctx.WithBlockHeader(cmtproto.Header{Height: 500})
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "frank", Owner: owner})
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "frank",
					Claimant:    claimant,
					FiledAt:     100,
					Active:      true,
					StakeAmount: math.NewInt(50),
				})
				f.mockRep.lockErr = types.ErrDREAMOperationFailed
			},
			expectPass: false,
			expectErr:  "DREAM token operation failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			cacheCtx, _ := f.ctx.CacheContext()

			_, err := ms.ContestDispute(cacheCtx, tc.msg)

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
