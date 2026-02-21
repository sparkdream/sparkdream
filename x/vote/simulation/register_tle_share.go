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

func SimulateMsgRegisterTLEShare(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRegisterTLEShare{}

		// Pick a random account to act as validator
		simAccount, _ := simtypes.RandomAcc(r, accs)
		validator := simAccount.Address.String()

		// Collect existing share indices to avoid duplicates
		usedIndices := make(map[uint64]bool)
		_ = k.TleValidatorShare.Walk(ctx, nil, func(_ string, vs types.TleValidatorShare) (bool, error) {
			usedIndices[vs.ShareIndex] = true
			return false, nil
		})

		// Pick a unique 1-based share index
		shareIndex := uint64(1)
		for usedIndices[shareIndex] {
			shareIndex++
		}

		// Store the TLE validator share directly (bypasses bonded-validator check)
		share := types.TleValidatorShare{
			Validator:      validator,
			PublicKeyShare: randomZKPublicKey(r),
			ShareIndex:     shareIndex,
			RegisteredAt:   ctx.BlockHeight(),
		}
		if err := k.TleValidatorShare.Set(ctx, validator, share); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to store TLE share: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
