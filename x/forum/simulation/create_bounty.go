package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgCreateBounty simulates a MsgCreateBounty message using direct keeper calls.
// This bypasses token escrow requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgCreateBounty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a root post for the bounty
		threadID, err := getOrCreateRootPostByAuthor(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateBounty{}), "failed to get/create root post"), nil, nil
		}

		// Use direct keeper calls to create bounty (bypasses token escrow)
		bountyID, err := k.BountySeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateBounty{}), "failed to get bounty ID"), nil, nil
		}

		now := ctx.BlockTime().Unix()
		amountVal := 50 + r.Intn(100)
		duration := int64(86400 * (r.Intn(24) + 7))

		bounty := types.Bounty{
			Id:        bountyID,
			Creator:   simAccount.Address.String(),
			ThreadId:  threadID,
			Amount:    fmt.Sprintf("%d", amountVal),
			CreatedAt: now,
			ExpiresAt: now + duration,
			Status:    types.BountyStatus_BOUNTY_STATUS_ACTIVE,
		}

		if err := k.Bounty.Set(ctx, bountyID, bounty); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateBounty{}), "failed to create bounty"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateBounty{}), "ok (direct keeper call)"), nil, nil
	}
}
