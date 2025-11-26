package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func SimulateMsgEmergencyCancelProposal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	gk *govkeeper.Keeper,
	gpK groupkeeper.Keeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Safety check for GovKeeper and its store
		if gk == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelProposal{}), "gov keeper is nil"), nil, nil
		}

		// 1. Get the authorized Council Address from params
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelProposal{}), "failed to get params"), nil, nil
		}
		councilAddrStr := params.CommonsCouncilAddress

		// 2. Find if we control this address
		var simAccount simtypes.Account
		var found bool

		councilAddr, err := sdk.AccAddressFromBech32(councilAddrStr)
		if err == nil {
			simAccount, found = simtypes.FindAccount(accs, councilAddr)
		}

		if !found {
			// If we can't sign as the authorized council, we pick a random account.
			// This effectively tests the "Unauthorized" error path.
			simAccount, _ = simtypes.RandomAcc(r, accs)
		}

		// 3. Find an Active Proposal ID using the GovKeeper
		var proposalID uint64
		foundProposal := false

		nextID, err := gk.ProposalID.Peek(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelProposal{}), "error peeking proposals"), nil, nil
		}

		// If nextID is 1, it means IDs 0 is unused and 1 is next, so 0 proposals exist.
		if nextID < 2 {
			// Safely iterate proposals using the Iterator API instead of Walk
			// We pass 'nil' to iterate over the entire range of proposals
			iter, err := gk.Proposals.Iterate(ctx, nil)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelProposal{}), "error getting proposal iterator"), nil, nil
			}
			defer iter.Close()

			// Loop through proposals to find a valid target
			for ; iter.Valid(); iter.Next() {
				prop, err := iter.Value()
				if err != nil {
					continue
				}

				// Check for active status
				if prop.Status == v1.StatusVotingPeriod || prop.Status == v1.StatusDepositPeriod {
					proposalID = prop.Id
					foundProposal = true
					break // Found one, stop searching
				}
			}
		}

		if !foundProposal {
			// Attempt to create a proposal so we have something to cancel.
			// This ensures the simulation is effective even if x/gov ops aren't running frequently.

			// A. Pick a proposer
			proposer, _ := simtypes.RandomAcc(r, accs)

			// B. Get Gov Params for MinDeposit
			govParams, err := gk.Params.Get(ctx)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelProposal{}), "failed to get gov params"), nil, nil
			}
			deposit := govParams.MinDeposit
			if len(deposit) == 0 {
				// Fallback if gov params aren't set correctly in simulation
				deposit = sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(10000000)))
			}

			// C. Create a dummy message (MsgSend to self)
			msgSend := &banktypes.MsgSend{
				FromAddress: proposer.Address.String(),
				ToAddress:   proposer.Address.String(),
				Amount:      sdk.NewCoins(sdk.NewCoin(deposit[0].Denom, math.NewInt(1))),
			}

			// D. Create MsgSubmitProposal
			submitMsg, err := v1.NewMsgSubmitProposal(
				[]sdk.Msg{msgSend},
				deposit,
				proposer.Address.String(),
				"Simulated Proposal",
				"Created by x/commons simulation to test cancellation",
				"Title",
				false, // expedited
			)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelProposal{}), "failed to create submit msg"), nil, nil
			}

			// E. Execute SubmitProposal
			setupOpMsg := simulation.OperationInput{
				R:               r,
				App:             app,
				TxGen:           txGen,
				Cdc:             nil,
				Msg:             submitMsg,
				CoinsSpentInMsg: deposit,
				Context:         ctx,
				SimAccount:      proposer,
				AccountKeeper:   ak,
				Bankkeeper:      bk,
				ModuleName:      types.ModuleName,
			}

			opResult, _, err := simulation.GenAndDeliverTxWithRandFees(setupOpMsg)

			// If we failed to create a proposal (e.g. insufficient funds), we just return NoOp and skip this step.
			if err != nil || !opResult.OK {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelProposal{}), "failed to seed proposal"), nil, nil
			}

			// F. Get the new Proposal ID
			// The proposal we just created should be the latest one (nextID - 1)
			nextID, _ := gk.ProposalID.Peek(ctx)
			if nextID > 0 {
				proposalID = nextID - 1
				foundProposal = true
			}
		}

		if !foundProposal {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelProposal{}), "no active proposal found"), nil, nil
		}

		msg := &types.MsgEmergencyCancelProposal{
			Authority:  simAccount.Address.String(),
			ProposalId: proposalID,
		}

		// 4. Construct the OperationInput
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: nil,
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		// 5. Execute
		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
