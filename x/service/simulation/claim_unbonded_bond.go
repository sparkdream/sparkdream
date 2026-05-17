package simulation

import (
	"math/rand"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgClaimUnbondedBond archives a random UNBONDING operator
// whose unbond_complete_at has elapsed.
func SimulateMsgClaimUnbondedBond(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, found := findOperatorWithStatus(r, ctx, k, types.OperatorStatus_OPERATOR_STATUS_UNBONDING)
		msg := &types.MsgClaimUnbondedBond{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no unbonding operator"), nil, nil
		}
		if ctx.BlockHeight() < op.UnbondCompleteAt {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "unbonding period not elapsed"), nil, nil
		}
		msg.Operator = op.Address
		msg.ServiceType = op.ServiceType

		op.Bond = sdk.NewCoin(types.BondDenom, sdkmath.ZeroInt())
		op.Status = types.OperatorStatus_OPERATOR_STATUS_RETIRED
		op.RetiredAt = ctx.BlockHeight()
		if err := k.ArchiveOperator(ctx, op); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to archive operator"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
