package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

// SimulateMsgPropose opens an update-only proposal (MsgUpdateVault with the
// vault's current configuration): it always executes successfully, needs no
// vault balance, and exercises the config_version bump + invalidation path.
func SimulateMsgPropose(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgPropose{})
		v, ok := randomVault(r, ctx, k)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no vault"), nil, nil
		}
		signers := signerAccounts(v, accs)
		if len(signers) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no simulation account signs for the vault"), nil, nil
		}
		signer := signers[r.Intn(len(signers))]
		_, updateOnly, err := k.PendingCounts(ctx, v.VaultId, signer.Address.String())
		if err != nil || updateOnly >= types.MaxPendingUpdateProposalsPerProposer {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "proposer at the update-proposal cap"), nil, nil
		}
		inner, err := codectypes.NewAnyWithValue(&types.MsgUpdateVault{
			Vault: v.Address, Signers: v.Signers, Threshold: v.Threshold, ProposalTtl: v.ProposalTtl, Label: v.Label,
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "pack inner msg"), nil, nil
		}
		msg := &types.MsgPropose{
			Signer: signer.Address.String(), VaultId: v.VaultId, Messages: []*codectypes.Any{inner},
			Memo: "sim rotation", Execute: r.Intn(2) == 0,
		}
		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R: r, App: app, TxGen: txGen, Cdc: nil, Msg: msg, CoinsSpentInMsg: sdk.NewCoins(),
			Context: ctx, SimAccount: signer, AccountKeeper: ak, Bankkeeper: bk, ModuleName: types.ModuleName,
		})
	}
}
