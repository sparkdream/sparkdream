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

// SimulateMsgTopUpBond adds bond directly to a random live operator.
func SimulateMsgTopUpBond(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, found := findRandomOperator(r, ctx, k)
		msg := &types.MsgTopUpBond{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no operator available"), nil, nil
		}
		if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNBONDING {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "operator is unbonding"), nil, nil
		}

		additional := sdk.NewCoin(types.BondDenom, sdkmath.NewInt(int64(1+r.Intn(1_000_000))))
		msg.Operator = op.Address
		msg.ServiceType = op.ServiceType
		msg.AdditionalBond = additional

		op.Bond = op.Bond.Add(additional)
		if err := k.PutOperator(ctx, op); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update operator"), nil, nil
		}
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
