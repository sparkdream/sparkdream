package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgPinPost simulates a MsgPinPost message using direct keeper calls.
// This bypasses the operations committee requirement for simulation purposes.
// Full integration testing should be done in integration tests.
//
// Under the strict-separation design, Pin requires a permanent target. The
// simulation collapses both state mutations (promote + pin) into one direct
// keeper write so the sim can exercise pinned-post invariants downstream
// without first wiring through the trust-gated MsgMakePostPermanent path.
func SimulateMsgPinPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find a root post to pin
		_, rootID, err := findRootPost(r, ctx, k)
		if err != nil {
			// Create one
			rootID, err = getOrCreateRootPost(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinPost{}), "failed to get/create root post"), nil, nil
			}
		}

		// Get the post as value type
		rootPost, err := k.Post.Get(ctx, rootID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinPost{}), "failed to get post"), nil, nil
		}

		// If ephemeral, run an equivalent of MakePostPermanent first so the
		// pin's ephemeral-block invariant holds. Mirrors the blog sim.
		if rootPost.ExpirationTime > 0 {
			_ = k.ExpirationQueue.Remove(ctx, collections.Join(rootPost.ExpirationTime, rootID))
			k.RemoveEphemeralAuthorIndex(ctx, rootPost.Author, rootID)
			rootPost.ExpirationTime = 0
		}

		// Use direct keeper calls to pin the post
		rootPost.Pinned = true
		if err := k.Post.Set(ctx, rootID, rootPost); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinPost{}), "failed to pin post"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinPost{}), "ok (direct keeper call)"), nil, nil
	}
}
