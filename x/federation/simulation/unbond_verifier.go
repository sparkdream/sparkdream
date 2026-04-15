package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgUnbondVerifier(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		verifier, err := getOrCreateVerifier(r, ctx, k, addr)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondVerifier{}), "failed to get/create verifier"), nil, nil
		}

		// Unbond part of the bond (not committed)
		available := verifier.CurrentBond.Sub(verifier.TotalCommittedBond)
		if available.LTE(math.ZeroInt()) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondVerifier{}), "no available bond to unbond"), nil, nil
		}

		unbondAmount := math.NewInt(int64(r.Intn(int(available.Int64())) + 1))
		verifier.CurrentBond = verifier.CurrentBond.Sub(unbondAmount)

		if err := k.Verifiers.Set(ctx, addr, verifier); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondVerifier{}), "failed to update verifier"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondVerifier{}), "ok (direct keeper call)"), nil, nil
	}
}
