package keeper_test

import (
	"testing"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestFileDispute(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	// Test addresses
	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	claimant := claimantAddr.String()
	ownerAddr := sdk.AccAddress([]byte("owner_address_______"))
	owner := ownerAddr.String()

	tests := []struct {
		name       string
		msg        *types.MsgFileDispute
		setup      func()
		expectPass bool
		expectErr  string
		check      func(t *testing.T, ctx sdk.Context)
	}{
		{
			name: "Success - DREAM stake locked and dispute created",
			msg: &types.MsgFileDispute{
				Authority: claimant,
				Name:      "alice",
				Reason:    "I registered this first",
			},
			setup: func() {
				f.mockRep.Reset()
				// Create name record owned by someone else
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "alice", Owner: owner})
				f.keeper.AddNameToOwner(f.ctx, ownerAddr, "alice")
			},
			expectPass: true,
			check: func(t *testing.T, ctx sdk.Context) {
				// Verify DREAM was locked
				locked := f.mockRep.LockedDREAM[claimant]
				require.True(t, locked.Equal(types.DefaultDisputeStakeDream), "Expected %s DREAM locked, got %s", types.DefaultDisputeStakeDream, locked)

				// Verify dispute record
				dispute, found := f.keeper.GetDispute(ctx, "alice")
				require.True(t, found)
				require.Equal(t, "alice", dispute.Name)
				require.Equal(t, claimant, dispute.Claimant)
				require.True(t, dispute.Active)
				require.True(t, dispute.StakeAmount.Equal(types.DefaultDisputeStakeDream))
				require.Empty(t, dispute.ContestChallengeId)
			},
		},
		{
			name: "Failure - Name not found",
			msg: &types.MsgFileDispute{
				Authority: claimant,
				Name:      "nonexistent",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
			},
			expectPass: false,
			expectErr:  "name not found",
		},
		{
			name: "Failure - Active dispute already exists",
			msg: &types.MsgFileDispute{
				Authority: claimant,
				Name:      "bob",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "bob", Owner: owner})
				f.keeper.AddNameToOwner(f.ctx, ownerAddr, "bob")
				// Create existing active dispute
				f.keeper.SetDispute(f.ctx, types.Dispute{
					Name:        "bob",
					Claimant:    claimant,
					Active:      true,
					StakeAmount: math.NewInt(50),
				})
			},
			expectPass: false,
			expectErr:  "active dispute already exists",
		},
		{
			name: "Failure - DREAM lock fails (insufficient DREAM)",
			msg: &types.MsgFileDispute{
				Authority: claimant,
				Name:      "carol",
				Reason:    "test",
			},
			setup: func() {
				f.mockRep.Reset()
				f.keeper.SetName(f.ctx, types.NameRecord{Name: "carol", Owner: owner})
				f.keeper.AddNameToOwner(f.ctx, ownerAddr, "carol")
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

			_, err := ms.FileDispute(cacheCtx, tc.msg)

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
