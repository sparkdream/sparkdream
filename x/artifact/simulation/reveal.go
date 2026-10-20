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

// SimulateMsgReveal reveals a random unrevealed token of a simulated blind
// class with the proof recomputed from the class id.
func SimulateMsgReveal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgReveal{}
		t, _, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return t.Unrevealed && t.Id <= simBlindLeaves && c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE &&
				t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no unrevealed token")
		}
		c, _ := k.Classes.Get(ctx, t.ClassId)
		owner, found := findAccount(accs, c.Owner)
		if !found {
			return noop(msg, "class owner is not a sim account")
		}
		root, proofs := simBlindTree(c.Id)
		if hex.EncodeToString(root) != c.Reveal.Commitment {
			return noop(msg, "class commitment is not the simulated tree")
		}
		msg = &types.MsgReveal{
			Signer: owner.Address.String(), ClassId: c.Id, TokenId: t.Id,
			Metadata: simBlindMetadata(c.Id, t.Id), Salt: hex.EncodeToString(simSalt(c.Id, t.Id)),
			Proof: types.HexHashes(proofs[t.Id-1]),
		}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
