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

func SimulateMsgCreateVault(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateVault{})
		params, err := k.Params.Get(ctx)
		if err != nil || !params.CreationEnabled {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "creation disabled"), nil, nil
		}
		creator, _ := simtypes.RandomAcc(r, accs)
		denom := k.BondDenom(ctx)
		spendable := bk.SpendableCoins(ctx, creator.Address)
		if spendable.AmountOf(denom).LT(params.CreationFeeAmount.AddRaw(1_000_000)) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "creator cannot pay the creation fee and gas"), nil, nil
		}
		// 1..3 distinct signers from the simulation accounts, weights 1..3.
		n := 1 + r.Intn(3)
		if n > len(accs) {
			n = len(accs)
		}
		perm := r.Perm(len(accs))
		signers := make([]types.VaultSigner, 0, n)
		var sum uint64
		for i := 0; i < n; i++ {
			w := uint64(1 + r.Intn(3))
			signers = append(signers, types.VaultSigner{Address: accs[perm[i]].Address.String(), Weight: w})
			sum += w
		}
		threshold := uint64(1 + r.Intn(int(sum)))
		ttl := types.MinProposalTTL + r.Int63n(types.MaxProposalTTL-types.MinProposalTTL)
		msg := &types.MsgCreateVault{
			Creator: creator.Address.String(), Signers: signers, Threshold: threshold,
			ProposalTtl: ttl, Label: simtypes.RandStringOfLength(r, 1+r.Intn(16)),
		}
		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R: r, App: app, TxGen: txGen, Cdc: nil, Msg: msg,
			CoinsSpentInMsg: sdk.NewCoins(sdk.NewCoin(denom, params.CreationFeeAmount)),
			Context:         ctx, SimAccount: creator, AccountKeeper: ak, Bankkeeper: bk, ModuleName: types.ModuleName,
		})
	}
}
