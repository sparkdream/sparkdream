package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgRetryScheduledOneshot picks a random
// PAUSED_INSUFFICIENT_FUNDS scheduled-oneshot grant and submits a
// retry from the granter or grantee (both are accepted by the
// handler).
//
// NoOps when no paused oneshot exists — which is the common case in
// randomized sims, since underfunded oneshots are rare without
// explicit bank churn. Kept for completeness so a sim that does
// generate PAUSED state through other operations sees the retry path
// exercised.
func SimulateMsgRetryScheduledOneshot(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgRetryScheduledOneshot{})

		grant, found := findRandomGrantOfType(
			r, ctx, k, types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT,
			func(g types.Grant) bool {
				return g.Status == types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
			},
		)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no paused oneshot to retry"), nil, nil
		}

		// Caller can be granter OR grantee. Pick whichever is a sim
		// account; if neither, NoOp.
		var callerAcc simtypes.Account
		var callerAddr string
		if acc, ok := findSimAccount(accs, grant.Granter); ok {
			callerAcc = acc
			callerAddr = grant.Granter
		} else if acc, ok := findSimAccount(accs, grant.Grantee); ok {
			callerAcc = acc
			callerAddr = grant.Grantee
		} else {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "neither granter nor grantee is a sim account"), nil, nil
		}

		msg := &types.MsgRetryScheduledOneshot{
			Caller:  callerAddr,
			GrantId: grant.Id,
		}

		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      callerAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
