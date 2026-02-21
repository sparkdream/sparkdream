package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func SimulateMsgRotateVoterKey(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRotateVoterKey{}

		// Pick a random account and ensure registered
		simAccount, _ := simtypes.RandomAcc(r, accs)
		if err := getOrCreateVoterRegistration(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to register voter"), nil, nil
		}

		// Rotate to a new random key
		reg, err := k.VoterRegistration.Get(ctx, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get registration"), nil, nil
		}

		reg.ZkPublicKey = randomZKPublicKey(r)
		reg.EncryptionPublicKey = randomZKPublicKey(r)

		if err := k.VoterRegistration.Set(ctx, simAccount.Address.String(), reg); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to rotate key"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
