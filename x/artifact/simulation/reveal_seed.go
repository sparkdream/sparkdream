package simulation

import (
	"encoding/hex"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgRevealSeed reveals the seed of a random unrevealed generative
// class whose commitment was derived from its id.
func SimulateMsgRevealSeed(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRevealSeed{}
		var candidates []types.Class
		_ = k.Classes.Walk(ctx, nil, func(_ uint64, c types.Class) (bool, error) {
			if c.Reveal.Kind == types.RevealKind_REVEAL_KIND_GENERATIVE_SEED && c.Reveal.RevealedAt == 0 &&
				c.Reveal.Commitment == types.SeedCommitment(simSeed(c.Id)) && c.Supply > 0 {
				candidates = append(candidates, c)
			}
			return len(candidates) >= 50, nil
		})
		if len(candidates) == 0 {
			return noop(msg, "no generative class awaiting its seed")
		}
		c := candidates[r.Intn(len(candidates))]
		owner, found := findAccount(accs, c.Owner)
		if !found {
			return noop(msg, "class owner is not a sim account")
		}
		msg = &types.MsgRevealSeed{Owner: owner.Address.String(), ClassId: c.Id, Seed: hex.EncodeToString(simSeed(c.Id))}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
