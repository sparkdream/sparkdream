package simulation

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgFileDispute simulates a MsgFileDispute message using direct keeper calls.
// This bypasses the DREAM token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgFileDispute(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	ck types.CommonsKeeper,
	gk types.GroupKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Create a name to dispute
		targetName := strings.ToLower(simtypes.RandStringOfLength(r, 10))
		targetOwner, _ := simtypes.RandomAcc(r, accs)

		// Collision check
		_, foundName := k.GetName(ctx, targetName)
		if foundName {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "name already exists"), nil, nil
		}

		record := types.NameRecord{
			Name:  targetName,
			Owner: targetOwner.Address.String(),
			Data:  simtypes.RandStringOfLength(r, 20),
		}

		if err := k.SetName(ctx, record); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to set name record"), nil, nil
		}
		if err := k.AddNameToOwner(ctx, targetOwner.Address, targetName); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to add name to owner"), nil, nil
		}

		// Create dispute record directly (bypasses DREAM lock requirement)
		params := k.GetParams(ctx)
		currentHeight := ctx.BlockHeight()
		dispute := types.Dispute{
			Name:        targetName,
			Claimant:    simAccount.Address.String(),
			FiledAt:     currentHeight,
			StakeAmount: params.DisputeStakeDream,
			Active:      true,
		}
		if err := k.SetDispute(ctx, dispute); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create dispute"), nil, nil
		}

		// Store DisputeStake record
		challengeID := fmt.Sprintf("name_dispute:%s:%d", targetName, currentHeight)
		disputeStake := types.DisputeStake{
			ChallengeId: challengeID,
			Staker:      simAccount.Address.String(),
			Amount:      params.DisputeStakeDream,
		}
		if err := k.DisputeStakes.Set(ctx, challengeID, disputeStake); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create dispute stake"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "ok (direct keeper call)"), nil, nil
	}
}
