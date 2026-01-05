package simulation

import (
	"math/rand"
	"slices"
	"strings"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func SimulateMsgEmergencyCancelGovProposal(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{}), "gov keeper is nil"), nil, nil
		}

		targetMsgType := sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{})

		// 1. Select a random account
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. DECISION: Happy Path vs Error Path
		// We randomly decide (50/50) if we want this transaction to succeed or fail.
		wantSuccess := r.Intn(2) == 0

		// 3. SETUP PERMISSIONS (Injection)
		// If we want success, we must ensure the account HAS permission.
		if wantSuccess {
			perms, err := k.PolicyPermissions.Get(ctx, simAccount.Address.String())
			var currentMsgs []string
			if err == nil {
				currentMsgs = perms.AllowedMessages
			}

			if !slices.Contains(currentMsgs, targetMsgType) {
				// Inject the permission so the transaction can succeed
				newPerms := types.PolicyPermissions{
					PolicyAddress:   simAccount.Address.String(),
					AllowedMessages: append(currentMsgs, targetMsgType),
				}
				if err := k.PolicyPermissions.Set(ctx, simAccount.Address.String(), newPerms); err != nil {
					return simtypes.NoOpMsg(types.ModuleName, targetMsgType, "failed to set permissions"), nil, err
				}
			}
		}

		// 4. FIND OR CREATE PROPOSAL
		var proposalID uint64
		foundProposal := false

		nextID, err := gk.ProposalID.Peek(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{}), "error peeking proposals"), nil, nil
		}

		if nextID < 2 {
			iter, err := gk.Proposals.Iterate(ctx, nil)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{}), "error getting proposal iterator"), nil, nil
			}
			defer iter.Close()

			for ; iter.Valid(); iter.Next() {
				prop, err := iter.Value()
				if err != nil {
					continue
				}
				if prop.Status == v1.StatusVotingPeriod || prop.Status == v1.StatusDepositPeriod {
					proposalID = prop.Id
					foundProposal = true
					break
				}
			}
		}

		if !foundProposal {
			// Seed a new proposal
			proposer, _ := simtypes.RandomAcc(r, accs)
			govParams, err := gk.Params.Get(ctx)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{}), "failed to get gov params"), nil, nil
			}
			deposit := govParams.MinDeposit
			if len(deposit) == 0 {
				deposit = sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(10000000)))
			}

			// Retrieve Governance Module Address
			govAddr := ak.GetModuleAddress(govtypes.ModuleName).String()

			// Create a dummy message
			msgSend := &banktypes.MsgSend{
				FromAddress: govAddr, // Must be Gov Module Address
				ToAddress:   proposer.Address.String(),
				Amount:      sdk.NewCoins(sdk.NewCoin(deposit[0].Denom, math.NewInt(1))),
			}

			submitMsg, err := v1.NewMsgSubmitProposal(
				[]sdk.Msg{msgSend},
				deposit,
				proposer.Address.String(),
				"Simulated Proposal",
				"Created by x/commons simulation to test cancellation",
				"Title",
				false,
			)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{}), "failed to create submit msg"), nil, nil
			}

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

			// Define explicit high fees to satisfy the AnteHandler check (5M uspark)
			fees := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5000000)))

			opResult, _, err := simulation.GenAndDeliverTx(setupOpMsg, fees)

			if err != nil || !opResult.OK {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{}), "failed to seed proposal"), nil, nil
			}

			nextID, _ := gk.ProposalID.Peek(ctx)
			if nextID > 0 {
				proposalID = nextID - 1
				foundProposal = true
			}
		}

		if !foundProposal {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{}), "no active proposal found"), nil, nil
		}

		msg := &types.MsgEmergencyCancelGovProposal{
			Authority:  simAccount.Address.String(),
			ProposalId: proposalID,
		}

		// 5. Construct the OperationInput
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

		// 6. EXECUTE
		opResult, futureOps, err := simulation.GenAndDeliverTxWithRandFees(opMsg)

		if err != nil {
			// If we expected success (we injected permissions) but got an error, return it (Test Failure)
			if wantSuccess {
				return opResult, futureOps, err
			}

			// If we expected failure (unauthorized) and got it, swallow it (Test Pass)
			if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "no permissions") {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "expected authorization failure: "+err.Error()), nil, nil
			}

			return opResult, futureOps, err
		}

		return opResult, futureOps, nil
	}
}
