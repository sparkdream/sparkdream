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

// SimulateMsgStakeNomination is not delivered. Stakes are taken only in the
// season's NOMINATION phase, which the simulated season never reaches, and
// the handler's staker trust-level check reads x/rep state the season sim
// cannot inspect. Covered by keeper tests and the season e2e suite.
func SimulateMsgStakeNomination(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txCfg client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgStakeNomination{}
		if season, err := k.Season.Get(ctx); err != nil || season.Status != types.SeasonStatus_SEASON_STATUS_NOMINATION {
			return noOp(msg, "season not in nomination phase")
		}
		return noOp(msg, "staker trust level check is outside the season sim's reach")
	}
}
