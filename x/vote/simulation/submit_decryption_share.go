package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func SimulateMsgSubmitDecryptionShare(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSubmitDecryptionShare{}

		// Pick a random account to act as validator
		simAccount, _ := simtypes.RandomAcc(r, accs)
		validator := simAccount.Address.String()

		// Ensure the validator has a registered TLE share
		has, err := k.TleValidatorShare.Has(ctx, validator)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to check TLE share: "+err.Error()), nil, nil
		}
		if !has {
			// Register a TLE share for this validator first
			usedIndices := make(map[uint64]bool)
			_ = k.TleValidatorShare.Walk(ctx, nil, func(_ string, vs types.TleValidatorShare) (bool, error) {
				usedIndices[vs.ShareIndex] = true
				return false, nil
			})
			shareIndex := uint64(1)
			for usedIndices[shareIndex] {
				shareIndex++
			}
			valShare := types.TleValidatorShare{
				Validator:      validator,
				PublicKeyShare: randomZKPublicKey(r),
				ShareIndex:     shareIndex,
				RegisteredAt:   ctx.BlockHeight(),
			}
			if err := k.TleValidatorShare.Set(ctx, validator, valShare); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to register TLE share: "+err.Error()), nil, nil
			}
		}

		// Find an unused epoch for this validator
		usedEpochs := make(map[uint64]bool)
		_ = k.TleDecryptionShare.Walk(ctx, nil, func(_ string, ds types.TleDecryptionShare) (bool, error) {
			if ds.Validator == validator {
				usedEpochs[ds.Epoch] = true
			}
			return false, nil
		})
		epoch := uint64(1)
		for usedEpochs[epoch] {
			epoch++
		}
		shareKey := fmt.Sprintf("%s/%d", validator, epoch)

		// Store decryption share directly (bypasses bonded-validator and correctness proof checks)
		ds := types.TleDecryptionShare{
			Index:       shareKey,
			Validator:   validator,
			Epoch:       epoch,
			Share:       randomZKPublicKey(r),
			SubmittedAt: ctx.BlockHeight(),
		}
		if err := k.TleDecryptionShare.Set(ctx, shareKey, ds); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to store decryption share: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
