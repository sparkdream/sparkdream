package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgUnstakeNomination delivers MsgUnstakeNomination for a sim
// account's nomination stake during the season's NOMINATION phase. Skips
// every other phase (the simulated season never leaves ACTIVE, so this is
// normally a NoOp) and stakes whose DREAM cannot be unlocked.
func SimulateMsgUnstakeNomination(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txCfg client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnstakeNomination{}

		if k.IsInMaintenanceMode(ctx) {
			return noOp(msg, "maintenance mode")
		}
		if season, err := k.Season.Get(ctx); err != nil || season.Status != types.SeasonStatus_SEASON_STATUS_NOMINATION {
			return noOp(msg, "season not in nomination phase")
		}
		var stakes []types.NominationStake
		if err := k.NominationStake.Walk(ctx, nil, func(_ string, s types.NominationStake) (bool, error) {
			if _, ok := getAccountForAddress(s.Staker, accs); ok {
				stakes = append(stakes, s)
			}
			return false, nil
		}); err != nil {
			return noOpErr(msg, err)
		}
		stake, ok := pick(r, stakes)
		if !ok {
			return noOp(msg, "no sim-account nomination stake")
		}
		if has, err := k.Nomination.Has(ctx, stake.NominationId); err != nil || !has {
			return noOp(msg, "nomination not found")
		}
		staker, _ := getAccountForAddress(stake.Staker, accs)
		msg.Creator, msg.NominationId = stake.Staker, stake.NominationId
		if err := dryRunMsg(app, ctx, msg); err != nil {
			return noOp(msg, "handler would reject: "+err.Error())
		}

		return deliver(r, app, ctx, txCfg, ak, bk, staker, msg)
	}
}
