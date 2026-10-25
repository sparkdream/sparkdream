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

func deliver(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, msg sdk.Msg, acc simtypes.Account, ak types.AuthKeeper, bk types.BankKeeper, txGen client.TxConfig) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
		R: r, App: app, TxGen: txGen, Cdc: nil, Msg: msg, CoinsSpentInMsg: sdk.NewCoins(),
		Context: ctx, SimAccount: acc, AccountKeeper: ak, Bankkeeper: bk, ModuleName: types.ModuleName,
	})
}

// freshTarget picks a sim account that is not a member, so it can be a
// migration target.
func freshTarget(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account, exclude string) (simtypes.Account, bool) {
	for i := 0; i < 8; i++ {
		acc, _ := simtypes.RandomAcc(r, accs)
		if acc.Address.String() == exclude || k.IsMember(ctx, acc.Address) || k.IsTombstoned(ctx, acc.Address.String()) {
			continue
		}
		// Other modules' freshness vetoes (season profile, names, grants...).
		if err := k.CheckMigrationTarget(ctx, exclude, acc.Address.String(), types.MigrationPath_MIGRATION_PATH_KEY); err != nil {
			continue
		}
		return acc, true
	}
	return simtypes.Account{}, false
}

func accountFor(accs []simtypes.Account, addr string) (simtypes.Account, bool) {
	for _, a := range accs {
		if a.Address.String() == addr {
			return a, true
		}
	}
	return simtypes.Account{}, false
}
