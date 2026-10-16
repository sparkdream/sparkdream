package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgRevokeInvitation revokes a PENDING invitation whose inviter is
// a simulation account. Every handler precondition is checked against live
// state (pending status, inviter match) so delivery cannot fail; with no
// revocable invitation the op is a no-op.
func SimulateMsgRevokeInvitation(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgRevokeInvitation{})

		byAddr := make(map[string]simtypes.Account, len(accs))
		for _, a := range accs {
			byAddr[a.Address.String()] = a
		}

		var candidates []types.Invitation
		_ = k.Invitation.Walk(ctx, nil, func(_ uint64, inv types.Invitation) (bool, error) {
			if inv.Status != types.InvitationStatus_INVITATION_STATUS_PENDING {
				return false, nil
			}
			if _, ok := byAddr[inv.Inviter]; ok {
				candidates = append(candidates, inv)
			}
			return false, nil
		})
		if len(candidates) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no pending invitation from a simulation account"), nil, nil
		}
		inv := candidates[r.Intn(len(candidates))]
		inviterAcc := byAddr[inv.Inviter]

		msg := &types.MsgRevokeInvitation{
			Inviter:      inv.Inviter,
			InvitationId: inv.Id,
		}
		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      inviterAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
