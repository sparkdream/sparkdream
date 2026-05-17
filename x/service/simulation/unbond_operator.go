package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgUnbondOperator transitions a random ACTIVE / UNDERFUNDED
// operator to UNBONDING via direct keeper write.
func SimulateMsgUnbondOperator(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, found := findRandomOperator(r, ctx, k)
		msg := &types.MsgUnbondOperator{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no operator available"), nil, nil
		}
		if op.Status != types.OperatorStatus_OPERATOR_STATUS_ACTIVE &&
			op.Status != types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "operator not in unbondable status"), nil, nil
		}
		msg.Operator = op.Address
		msg.ServiceType = op.ServiceType

		cfg, err := k.ServiceTypes.Get(ctx, op.ServiceType)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "missing service-type config"), nil, nil
		}

		op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
		op.UnbondCompleteAt = ctx.BlockHeight() + cfg.UnbondingPeriodBlocks
		op.UnderfundedSince = 0
		if err := k.PutOperator(ctx, op); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update operator"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
