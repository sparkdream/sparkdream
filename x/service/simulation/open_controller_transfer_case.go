package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgOpenControllerTransferCase writes a synthetic
// ControllerTransferCase row keyed on a per-block placeholder, matching
// the standalone-mode branch of the msg-server handler.
func SimulateMsgOpenControllerTransferCase(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, found := findRandomOperator(r, ctx, k)
		msg := &types.MsgOpenControllerTransferCase{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no operator available"), nil, nil
		}

		opener, _ := simtypes.RandomAcc(r, accs)
		proposed, _ := simtypes.RandomAcc(r, accs)
		if proposed.Address.String() == op.Controller {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "proposed equals current controller"), nil, nil
		}

		opBytes := mustAccBytes(op.Address)
		if _, err := k.OpenControllerTransferByOperator.Get(ctx, collections.Join(opBytes, op.ServiceType)); err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "case already open"), nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to load params"), nil, nil
		}

		msg.Opener = opener.Address.String()
		msg.Operator = op.Address
		msg.ServiceType = op.ServiceType
		msg.ProposedNewController = proposed.Address.String()
		msg.Reason = "sim-transfer"

		// Synthetic id matches the standalone-branch shape in the msg-server
		// handler: (height << 16) | reason-len. Collision-free across sim
		// blocks.
		height := ctx.BlockHeight()
		caseID := uint64(height)<<16 | uint64(len(msg.Reason)&0xFFFF)

		caseRow := types.ControllerTransferCase{
			JuryCaseId:            caseID,
			OperatorAddress:       msg.Operator,
			ServiceType:           msg.ServiceType,
			Opener:                msg.Opener,
			ProposedNewController: msg.ProposedNewController,
			Deposit:               params.ReportDepositAmount,
			OpenedAt:              height,
		}
		_ = k.ControllerTransferCases.Set(ctx, caseID, caseRow)
		_ = k.OpenControllerTransferByOperator.Set(ctx, collections.Join(opBytes, op.ServiceType), caseID)

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
