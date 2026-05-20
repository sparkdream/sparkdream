package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgExecSession simulates a session key execution using direct keeper
// calls. GenAndDeliverTx cannot be used here because the inner message dispatch
// (e.g., blog MsgCreatePost) requires cross-module state (rep membership) that
// the session simulation cannot seed. This matches the pattern used by x/blog
// and x/forum simulations.
func SimulateMsgExecSession(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgExecSession{})

		// Get or create a zero-spend-limit session with exec budget
		session, _, _, err := getOrCreateSession(r, ctx, k, accs, true)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get or create session: "+err.Error()), nil, nil
		}

		// Simulate the execution directly via keeper on the backing
		// SESSION_KEY grant: increment exec_count and update last_used_at,
		// same as the ExecSession handler does.
		id, err := k.SessionKeyByPair.Get(ctx, collections.Join(session.Granter, session.Grantee))
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, fmt.Sprintf("session lookup failed: %v", err)), nil, nil
		}
		grant, err := k.Grants.Get(ctx, id)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, fmt.Sprintf("grant lookup failed: %v", err)), nil, nil
		}
		sk := grant.GetSessionKey()
		if sk == nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "grant is not a session key"), nil, nil
		}
		sk.ExecCount++
		sk.LastUsedAt = ctx.BlockTime()
		grant.Payload = &types.Grant_SessionKey{SessionKey: sk}
		if err := k.Grants.Set(ctx, id, grant); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, fmt.Sprintf("failed to update grant: %v", err)), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
