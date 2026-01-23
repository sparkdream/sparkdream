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

func SimulateMsgAcceptInvitation(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create an inviter
		inviter, _, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptInvitation{}), "failed to get/create inviter"), nil, nil
		}

		// Find an invitee that exists in our simulation accounts but is NOT already a member
		var inviteeAcc simtypes.Account
		inviteeFound := false
		for i := 0; i < 10; i++ {
			inviteeAcc, _ = simtypes.RandomAcc(r, accs)
			if inviteeAcc.Address.String() == inviter.Address {
				continue
			}
			// Check if invitee is already a member
			_, err := k.Member.Get(ctx, inviteeAcc.Address.String())
			if err != nil {
				// Not a member yet - this is a valid invitee
				inviteeFound = true
				break
			}
		}
		if !inviteeFound {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptInvitation{}), "unable to find non-member invitee"), nil, nil
		}

		// Try to find an existing invitation for this specific invitee
		var invitationID uint64
		found := false
		k.Invitation.Walk(ctx, nil, func(id uint64, inv types.Invitation) (bool, error) {
			if inv.Status == types.InvitationStatus_INVITATION_STATUS_PENDING &&
				inv.InviteeAddress == inviteeAcc.Address.String() {
				invitationID = id
				found = true
				return true, nil
			}
			return false, nil
		})

		if !found {
			// Create invitation for this specific invitee
			invitationID, err = createInvitation(ctx, k, r, inviter, inviteeAcc.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptInvitation{}), "failed to create invitation"), nil, nil
			}
		}

		msg := &types.MsgAcceptInvitation{
			Invitee:      inviteeAcc.Address.String(),
			InvitationId: invitationID,
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      inviteeAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
