package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func SimulateMsgCreatePolicyPermissions(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Set PolicyAddress = Authority to satisfy the "Self-Regulation" check in isAuthorized.
		msg := &types.MsgCreatePolicyPermissions{
			Authority:     simAccount.Address.String(),
			PolicyAddress: simAccount.Address.String(),
			// Optionally add some random allowed messages for realism
			AllowedMessages: []string{"/cosmos.group.v1.MsgVote"},
		}

		found, err := k.PolicyPermissions.Has(ctx, msg.PolicyAddress)
		if err == nil && found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "PolicyPermissions already exist"), nil, nil
		}

		txCtx := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			Context:         ctx,
			SimAccount:      simAccount,
			ModuleName:      types.ModuleName,
			CoinsSpentInMsg: sdk.NewCoins(),
			AccountKeeper:   ak,
			Bankkeeper:      bk,
		}
		return simulation.GenAndDeliverTxWithRandFees(txCtx)
	}
}

func SimulateMsgUpdatePolicyPermissions(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var (
			simAccount        = simtypes.Account{}
			policyPermissions = types.PolicyPermissions{}
			msg               = &types.MsgUpdatePolicyPermissions{}
			found             = false
		)

		var allPolicyPermissions []types.PolicyPermissions
		err := k.PolicyPermissions.Walk(ctx, nil, func(key string, value types.PolicyPermissions) (stop bool, err error) {
			allPolicyPermissions = append(allPolicyPermissions, value)
			return false, nil
		})
		if err != nil {
			panic(err)
		}

		// Find the account that matches the PolicyAddress so it can sign the update as "Self-Regulation".
		for _, obj := range allPolicyPermissions {
			acc, err := ak.AddressCodec().StringToBytes(obj.PolicyAddress)
			if err != nil {
				// If the policy address isn't a valid account address we can simulate, skip it
				continue
			}

			simAccount, found = simtypes.FindAccount(accs, sdk.AccAddress(acc))
			if found {
				policyPermissions = obj
				break
			}
		}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "policyPermissions authority not found in sim accounts"), nil, nil
		}

		msg.Authority = simAccount.Address.String()
		msg.PolicyAddress = policyPermissions.PolicyAddress
		msg.AllowedMessages = policyPermissions.AllowedMessages // Keep existing or modify randomly

		txCtx := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			Context:         ctx,
			SimAccount:      simAccount,
			ModuleName:      types.ModuleName,
			CoinsSpentInMsg: sdk.NewCoins(),
			AccountKeeper:   ak,
			Bankkeeper:      bk,
		}
		return simulation.GenAndDeliverTxWithRandFees(txCtx)
	}
}

func SimulateMsgDeletePolicyPermissions(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var (
			simAccount        = simtypes.Account{}
			policyPermissions = types.PolicyPermissions{}
			msg               = &types.MsgDeletePolicyPermissions{}
			found             = false
		)

		var allPolicyPermissions []types.PolicyPermissions
		err := k.PolicyPermissions.Walk(ctx, nil, func(key string, value types.PolicyPermissions) (stop bool, err error) {
			allPolicyPermissions = append(allPolicyPermissions, value)
			return false, nil
		})
		if err != nil {
			panic(err)
		}

		// Find the account matching PolicyAddress.
		for _, obj := range allPolicyPermissions {
			acc, err := ak.AddressCodec().StringToBytes(obj.PolicyAddress)
			if err != nil {
				continue
			}

			simAccount, found = simtypes.FindAccount(accs, sdk.AccAddress(acc))
			if found {
				policyPermissions = obj
				break
			}
		}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "policyPermissions authority not found in sim accounts"), nil, nil
		}

		msg.Authority = simAccount.Address.String()
		msg.PolicyAddress = policyPermissions.PolicyAddress

		txCtx := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			Context:         ctx,
			SimAccount:      simAccount,
			ModuleName:      types.ModuleName,
			CoinsSpentInMsg: sdk.NewCoins(),
			AccountKeeper:   ak,
			Bankkeeper:      bk,
		}
		return simulation.GenAndDeliverTxWithRandFees(txCtx)
	}
}
