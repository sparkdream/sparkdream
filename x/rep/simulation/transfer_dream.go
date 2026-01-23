package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgTransferDream(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a sender with DREAM
		minAmount := math.NewInt(10)
		sender, senderAcc, err := getOrCreateMemberWithDream(r, ctx, k, accs, minAmount)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferDream{}), "failed to get/create sender with DREAM"), nil, nil
		}

		// Get or create a recipient (different from sender)
		recipient, _, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferDream{}), "failed to get/create recipient"), nil, nil
		}
		// Ensure recipient is different from sender
		for i := 0; i < 10 && recipient.Address == sender.Address; i++ {
			recipient, _, err = getOrCreateMember(r, ctx, k, accs)
			if err != nil {
				break
			}
		}
		if recipient.Address == sender.Address {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferDream{}), "unable to find different recipient"), nil, nil
		}

		// Determine transfer purpose and amount based on limits
		// Tips: max 100 DREAM
		// Gifts: max 500 DREAM (for invitees)
		purpose := types.TransferPurpose_TRANSFER_PURPOSE_TIP
		maxTransfer := math.NewInt(100)

		// Randomly choose between tip and gift
		// Gifts are only allowed to invitees, so check the invitation relationship
		if r.Intn(2) == 0 && recipient.InvitedBy == sender.Address {
			purpose = types.TransferPurpose_TRANSFER_PURPOSE_GIFT
			maxTransfer = math.NewInt(500)
		}

		// Calculate transfer amount
		if (*sender.DreamBalance).LT(maxTransfer) {
			maxTransfer = *sender.DreamBalance
		}
		if maxTransfer.LT(minAmount) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferDream{}), "insufficient balance for transfer"), nil, nil
		}

		transferAmount := math.NewInt(int64(r.Intn(int(maxTransfer.Sub(minAmount).Int64()))) + minAmount.Int64())

		msg := &types.MsgTransferDream{
			Sender:    sender.Address,
			Recipient: recipient.Address,
			Amount:    &transferAmount,
			Purpose:   purpose,
			Reference: "simulation transfer",
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      senderAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
