package simulation

import (
	"math/rand"
	"slices"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgEmergencyCancelGovProposal simulates the permission setup for
// emergency cancel operations using direct keeper calls.
// The full proposal cancellation flow requires x/gov keeper integration which
// is not available in the simulation context (passed as nil). This simulation
// exercises the RBAC permission injection logic instead.
func SimulateMsgEmergencyCancelGovProposal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	gk *govkeeper.Keeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		targetMsgType := sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{})

		// 1. Select a random account
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. Grant emergency cancel permission to this account
		perms, err := k.PolicyPermissions.Get(ctx, simAccount.Address.String())
		var currentMsgs []string
		if err == nil {
			currentMsgs = perms.AllowedMessages
		}

		if !slices.Contains(currentMsgs, targetMsgType) {
			newPerms := types.PolicyPermissions{
				PolicyAddress:   simAccount.Address.String(),
				AllowedMessages: append(currentMsgs, targetMsgType),
			}
			if err := k.PolicyPermissions.Set(ctx, simAccount.Address.String(), newPerms); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, targetMsgType, "failed to set permissions"), nil, nil
			}
		}

		// 3. Verify the permission was stored correctly
		stored, err := k.PolicyPermissions.Get(ctx, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, targetMsgType, "failed to verify permissions"), nil, nil
		}
		if !slices.Contains(stored.AllowedMessages, targetMsgType) {
			return simtypes.NoOpMsg(types.ModuleName, targetMsgType, "permission not found after set"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, targetMsgType, "ok (direct keeper call)"), nil, nil
	}
}
