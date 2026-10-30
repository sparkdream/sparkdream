package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

// SimulateMsgLeaveVault declines a pending listing whose removal keeps the
// vault's threshold reachable (an accepted leave is exercised by unit tests;
// here it would mostly be refused by the anti-brick rule).
func SimulateMsgLeaveVault(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgLeaveVault{})
		v, ok := randomVault(r, ctx, k)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no vault"), nil, nil
		}
		invited := invitedAccounts(v, accs)
		if len(invited) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no simulated invitee"), nil, nil
		}
		signer := invited[r.Intn(len(invited))]
		var rest uint64
		for _, s := range v.Signers {
			if s.Address != signer.Address.String() {
				rest += s.Weight
			}
		}
		if len(v.Signers) < 2 || rest < v.Threshold {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "declining would make the threshold unreachable"), nil, nil
		}
		msg := &types.MsgLeaveVault{Signer: signer.Address.String(), VaultId: v.VaultId}
		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R: r, App: app, TxGen: txGen, Cdc: nil, Msg: msg, CoinsSpentInMsg: sdk.NewCoins(),
			Context: ctx, SimAccount: signer, AccountKeeper: ak, Bankkeeper: bk, ModuleName: types.ModuleName,
		})
	}
}
