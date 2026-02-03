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

// SimulateMsgIncreaseBounty simulates a MsgIncreaseBounty message using direct keeper calls.
// This bypasses token escrow requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgIncreaseBounty(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgIncreaseBounty{}), "failed to get/create bounty"), nil, nil
		}

		// Use direct keeper calls to increase bounty (bypasses token escrow)
		bounty, err := k.Bounty.Get(ctx, bountyID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgIncreaseBounty{}), "bounty not found"), nil, nil
		}

		// Increase the amount
		increaseVal := 10 + r.Intn(41)
		currentAmount, _ := math.NewIntFromString(bounty.Amount)
		if bounty.Amount == "" {
			currentAmount = math.ZeroInt()
		}
		newAmount := currentAmount.Add(math.NewInt(int64(increaseVal)))
		bounty.Amount = fmt.Sprintf("%d", newAmount.Int64())

		if err := k.Bounty.Set(ctx, bountyID, bounty); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgIncreaseBounty{}), "failed to increase bounty"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgIncreaseBounty{}), "ok (direct keeper call)"), nil, nil
	}
}
