package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// TestMaintenanceModeBlocksReputationMoves: while x/season reports
// maintenance mode, stake/unstake, initiative creation/completion and
// challenges are refused up front with ErrMaintenanceMode.
func TestMaintenanceModeBlocksReputationMoves(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetSeasonKeeper(mockSeasonKeeper{MaintenanceMode: true})
	srv := keeper.NewMsgServerImpl(f.keeper)
	addr := sdk.AccAddress([]byte("maintenance_member__")).String()
	amt := math.NewInt(1)

	_, err := srv.Stake(f.ctx, &types.MsgStake{Staker: addr, Amount: &amt})
	require.ErrorIs(t, err, types.ErrMaintenanceMode)
	_, err = srv.Unstake(f.ctx, &types.MsgUnstake{Staker: addr, StakeId: 1})
	require.ErrorIs(t, err, types.ErrMaintenanceMode)
	_, err = srv.CreateInitiative(f.ctx, &types.MsgCreateInitiative{Creator: addr})
	require.ErrorIs(t, err, types.ErrMaintenanceMode)
	_, err = srv.CompleteInitiative(f.ctx, &types.MsgCompleteInitiative{Creator: addr, InitiativeId: 1})
	require.ErrorIs(t, err, types.ErrMaintenanceMode)
	_, err = srv.CreateChallenge(f.ctx, &types.MsgCreateChallenge{Challenger: addr, StakedDream: &amt})
	require.ErrorIs(t, err, types.ErrMaintenanceMode)

	// Out of maintenance the same calls fail for their own reasons, not the gate.
	f.keeper.SetSeasonKeeper(mockSeasonKeeper{})
	_, err = srv.Stake(f.ctx, &types.MsgStake{Staker: addr, Amount: &amt})
	require.Error(t, err)
	require.NotErrorIs(t, err, types.ErrMaintenanceMode)
}
