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

// SimulateMsgRegisterOperator simulates a MsgRegisterOperator using
// direct keeper calls. Bypasses controller-group validation and bond
// escrow — full validation is exercised by the e2e shell suite.
func SimulateMsgRegisterOperator(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		controller, _ := simtypes.RandomAcc(r, accs)
		bondAmount := sdkmath.NewInt(1_000_000)
		msg := &types.MsgRegisterOperator{
			Creator:     simAccount.Address.String(),
			ServiceType: simServiceType,
			Controller:  controller.Address.String(),
			BondAmount:  bondAmount,
			Metadata:    []byte("sim-metadata"),
		}
		if simAccount.Address.Equals(controller.Address) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "self-controller skipped"), nil, nil
		}

		cfg, err := ensureServiceType(ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to ensure service type"), nil, nil
		}

		addrBytes := simAccount.Address.Bytes()
		if _, exists := k.GetOperator(ctx, addrBytes, simServiceType); exists {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "operator already registered"), nil, nil
		}

		height := ctx.BlockHeight()
		op := types.Operator{
			Address:                 msg.Creator,
			ServiceType:             cfg.ServiceType,
			Controller:              msg.Controller,
			BondAmount:              msg.BondAmount,
			Metadata:                msg.Metadata,
			Status:                  types.OperatorStatus_OPERATOR_STATUS_ACTIVE,
			Tier1SlashedInWindow:    sdkmath.ZeroInt(),
			Tier1WindowStart:        height,
			Tier1WindowStartBond:    msg.BondAmount,
			RegisteredAt:            height,
			TotalLifetimeBondBlocks: sdkmath.ZeroInt(),
			LastBondBlockUpdateAt:   height,
		}
		if err := k.PutOperator(ctx, op); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to put operator"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
