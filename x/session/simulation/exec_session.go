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

		// Simulate the execution directly via keeper: increment exec_count
		// and update last_used_at, same as the ExecSession handler does.
		session.ExecCount++
		session.LastUsedAt = ctx.BlockTime()

		key := collections.Join(session.Granter, session.Grantee)
		if err := k.Sessions.Set(ctx, key, session); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, fmt.Sprintf("failed to update session: %v", err)), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
