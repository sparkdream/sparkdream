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

// SimulateMsgNominate is not delivered. Nominations open only in the
// season's NOMINATION phase, which the simulated season (ending far beyond
// any run) never reaches, and the handler's nominator trust-level and
// content-author membership checks read x/rep and content keepers the
// season sim cannot inspect. Covered by keeper tests and the season e2e
// suite.
func SimulateMsgNominate(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txCfg client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgNominate{}
		if season, err := k.Season.Get(ctx); err != nil || season.Status != types.SeasonStatus_SEASON_STATUS_NOMINATION {
			return noOp(msg, "season not in nomination phase")
		}
		return noOp(msg, "nominator trust level and content checks are outside the season sim's reach")
	}
}
