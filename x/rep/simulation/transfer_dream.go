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

		// Pre-validation: the largest tip below must still fit in the sender's
		// epoch tip allowance.
		params, err := k.Params.Get(ctx)
		if err != nil {
			params = types.DefaultParams()
		}
		epoch, err := k.GetCurrentEpoch(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferDream{}), "failed to read epoch"), nil, nil
		}
		if sender.LastTipEpoch == epoch &&
			keeper.DerefInt(sender.TipsSentThisEpoch).AddRaw(100).GT(params.MaxTipsSentPerEpoch) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferDream{}), "exceeded tip allowance this epoch"), nil, nil
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

		// Recipient-side limit: skip rather than fail when the recipient has
		// no room left this epoch or season.
		rcpt := *recipient
		headroom, err := k.TransferReceiveHeadroom(ctx, params, &rcpt)
		if err != nil || headroom.LT(math.NewInt(100)) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferDream{}), "recipient at transfer receive limit"), nil, nil
		}

		// Use TIP transfers only (gifts are invitee-only with a lifetime cap,
		// which random accounts rarely satisfy). At most 100 micro-DREAM.
		purpose := types.TransferPurpose_TRANSFER_PURPOSE_TIP
		maxTransfer := math.NewInt(100)

		// The msg handler enforces DreamBalance - StakedDream >= amount and also
		// applies pending decay before the check, so stay below the raw unlocked
		// balance by a small safety margin to avoid spurious failures.
		staked := math.ZeroInt()
		if sender.StakedDream != nil {
			staked = *sender.StakedDream
		}
		unlocked := sender.DreamBalance.Sub(staked)
		// Reserve ~1% of unlocked balance (min 1) to cover decay between
		// this check and message delivery.
		buffer := unlocked.QuoRaw(100)
		if buffer.LT(math.OneInt()) {
			buffer = math.OneInt()
		}
		safeMax := unlocked.Sub(buffer)
		if safeMax.LT(maxTransfer) {
			maxTransfer = safeMax
		}
		if maxTransfer.LT(minAmount) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferDream{}), "insufficient balance for transfer"), nil, nil
		}

		// Calculate transfer amount (handle case where maxTransfer == minAmount)
		var transferAmount math.Int
		rangeVal := maxTransfer.Sub(minAmount).Int64()
		if rangeVal <= 0 {
			transferAmount = minAmount
		} else {
			transferAmount = math.NewInt(int64(r.Intn(int(rangeVal))) + minAmount.Int64())
		}

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
