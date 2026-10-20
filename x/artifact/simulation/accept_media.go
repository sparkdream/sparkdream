package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// randomProposal picks a random open media proposal whose token's holder is
// a sim account.
func randomProposal(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account) (types.MediaProposal, types.Token, simtypes.Account, bool) {
	var candidates []types.MediaProposal
	_ = k.MediaProposals.Walk(ctx, nil, func(_ collections.Triple[uint64, uint64, uint64], mp types.MediaProposal) (bool, error) {
		candidates = append(candidates, mp)
		return len(candidates) >= 200, nil
	})
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, mp := range candidates {
		t, err := k.Tokens.Get(ctx, collections.Join(mp.ClassId, mp.TokenId))
		if err != nil {
			continue
		}
		if acc, ok := findAccount(accs, t.Owner); ok {
			return mp, t, acc, true
		}
	}
	return types.MediaProposal{}, types.Token{}, simtypes.Account{}, false
}

// SimulateMsgAcceptMedia accepts a random open media proposal as the holder.
func SimulateMsgAcceptMedia(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAcceptMedia{}
		mp, t, holder, ok := randomProposal(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no open media proposal")
		}
		if uint32(len(t.MediaVersions)) >= k.GetParams(ctx).MaxMediaVersionsPerToken {
			return noop(msg, "token media versions full")
		}
		msg = &types.MsgAcceptMedia{Owner: holder.Address.String(), ClassId: mp.ClassId, TokenId: mp.TokenId, ProposalId: mp.Id, Display: r.Intn(2) == 0}
		return deliver(r, app, ctx, ak, bk, txGen, holder, msg, math.ZeroInt())
	}
}
