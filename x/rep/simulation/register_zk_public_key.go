package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgRegisterZkPublicKey(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		member, simAccount, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterZkPublicKey{}), "failed to get/create member"), nil, nil
		}

		// Generate a random 32-byte ZK public key
		zkPubKey := make([]byte, 32)
		r.Read(zkPubKey)

		msg := &types.MsgRegisterZkPublicKey{
			Member:      member.Address,
			ZkPublicKey: zkPubKey,
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
