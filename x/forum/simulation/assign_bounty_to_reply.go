package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgAssignBountyToReply simulates a MsgAssignBountyToReply message using direct keeper calls.
// This bypasses token transfer and escrow requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgAssignBountyToReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a bounty
		bountyID, err := getOrCreateBounty(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignBountyToReply{}), "failed to get/create bounty"), nil, nil
		}

		// Use direct keeper calls to assign bounty to reply (bypasses token transfer)
		bounty, err := k.Bounty.Get(ctx, bountyID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignBountyToReply{}), "bounty not found"), nil, nil
		}

		// Find or create a reply in the bounty's thread
		replyID, err := getOrCreateReply(r, ctx, k, bounty.ThreadId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignBountyToReply{}), "failed to get/create reply"), nil, nil
		}

		// Get the reply to find its author
		reply, err := k.Post.Get(ctx, replyID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignBountyToReply{}), "reply not found"), nil, nil
		}

		// Calculate award amount (portion of remaining funds)
		totalAmount, ok := math.NewIntFromString(bounty.Amount)
		if !ok || !totalAmount.IsPositive() {
			totalAmount = math.NewInt(100)
		}
		assignedAmount := math.ZeroInt()
		for _, a := range bounty.Awards {
			awardAmt, ok := math.NewIntFromString(a.Amount)
			if ok {
				assignedAmount = assignedAmount.Add(awardAmt)
			}
		}
		remainingAmount := totalAmount.Sub(assignedAmount)
		if !remainingAmount.IsPositive() {
			remainingAmount = math.NewInt(50)
		}

		// Award a portion of remaining (25-75%)
		awardPct := 25 + r.Intn(51)
		awardAmount := remainingAmount.MulRaw(int64(awardPct)).QuoRaw(100)
		if !awardAmount.IsPositive() {
			awardAmount = math.NewInt(10)
		}

		// Add award to bounty (field is PostId, not ReplyId)
		award := &types.BountyAward{
			PostId:    replyID,
			Recipient: reply.Author,
			Amount:    fmt.Sprintf("%d", awardAmount.Int64()),
			AwardedAt: ctx.BlockTime().Unix(),
			Reason:    "Excellent solution provided in simulation",
		}
		bounty.Awards = append(bounty.Awards, award)

		if err := k.Bounty.Set(ctx, bountyID, bounty); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignBountyToReply{}), "failed to assign bounty"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignBountyToReply{}), "ok (direct keeper call)"), nil, nil
	}
}
