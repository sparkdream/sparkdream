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

// SimulateMsgWithdrawBond queues a partial withdrawal on a random ACTIVE
// operator that has spare bond above min_bond (direct keeper call, like
// the other x/service sims).
func SimulateMsgWithdrawBond(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, found := findRandomOperator(r, ctx, k)
		msg := &types.MsgWithdrawBond{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no operator available"), nil, nil
		}
		if op.Status != types.OperatorStatus_OPERATOR_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "operator is not active"), nil, nil
		}
		cfg, err := k.ServiceTypes.Get(ctx, op.ServiceType)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "service type not found"), nil, nil
		}
		pending := sdkmath.ZeroInt()
		if !op.PendingWithdrawAmount.IsNil() {
			pending = op.PendingWithdrawAmount
		}
		spare := op.BondAmount.Sub(cfg.MinBondAmount).Sub(pending)
		if !spare.IsPositive() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no spare bond above min_bond"), nil, nil
		}
		amount := sdkmath.NewInt(1 + r.Int63n(spare.Int64()))
		msg.Operator = op.Address
		msg.ServiceType = op.ServiceType
		msg.Amount = amount

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "params not found"), nil, nil
		}
		period := cfg.UnbondingPeriodBlocks
		if period <= 0 {
			period = params.DefaultUnbondingPeriodBlocks
		}
		op.PendingWithdrawAmount = pending.Add(amount)
		op.WithdrawCompleteAt = ctx.BlockHeight() + period
		if err := k.PutOperator(ctx, op); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update operator"), nil, nil
		}
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
