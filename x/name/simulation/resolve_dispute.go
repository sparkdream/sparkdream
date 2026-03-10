package simulation

import (
	"fmt"
	"math/rand"

	commonstypes "sparkdream/x/commons/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgResolveDispute simulates a MsgResolveDispute using direct keeper calls.
// This bypasses the DREAM token requirement for simulation purposes, similar to
// SimulateMsgFileDispute. Full token integration testing should be done in
// integration tests.
func SimulateMsgResolveDispute(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	ck types.CommonsKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 1. Setup Infrastructure: Create council with simAccount as policy
		policyAddr := simAccount.Address.String()

		mockGroup := commonstypes.Group{
			GroupId:       uint64(simtypes.RandIntBetween(r, 1, 1000)),
			PolicyAddress: policyAddr,
		}
		if err := ck.SetGroup(ctx, CouncilName, mockGroup); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to set group"), nil, nil
		}

		// 2. Setup Target: Create a unique name + active dispute
		// Use block height + random suffix to avoid collisions across blocks
		disputeName := fmt.Sprintf("rd%d%s", ctx.BlockHeight(), simtypes.RandStringOfLength(r, 6))

		nameRecord := types.NameRecord{
			Name:  disputeName,
			Owner: simAccount.Address.String(),
			Data:  "disputed data",
		}
		if err := k.SetName(ctx, nameRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to set name record"), nil, nil
		}

		disputeRecord := types.Dispute{
			Name:     disputeName,
			Claimant: simAccount.Address.String(),
			Active:   true,
		}
		if err := k.SetDispute(ctx, disputeRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to set dispute"), nil, nil
		}

		// 3. Resolve dispute directly via keeper (dismiss - no transfer)
		disputeRecord.Active = false
		if err := k.SetDispute(ctx, disputeRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to resolve dispute"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "ok (direct keeper call)"), nil, nil
	}
}
