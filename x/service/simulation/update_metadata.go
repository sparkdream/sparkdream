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

// SimulateMsgUpdateMetadata flips the metadata on a random live
// operator via direct keeper write.
func SimulateMsgUpdateMetadata(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, found := findRandomOperator(r, ctx, k)
		msg := &types.MsgUpdateMetadata{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no operator available"), nil, nil
		}
		msg.Operator = op.Address
		msg.ServiceType = op.ServiceType
		msg.NewMetadata = []byte("sim-metadata-updated")

		op.Metadata = msg.NewMetadata
		if err := k.PutOperator(ctx, op); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update operator"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
