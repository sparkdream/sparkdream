package simulation

import (
	"math/rand"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commonskeeper "sparkdream/x/commons/keeper"
	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgResolveDispute(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	ck commonskeeper.Keeper,
	gk groupkeeper.Keeper,
	k keeper.Keeper,
	cdc codec.Codec,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Setup Actor: Pick a random account to be the Council Member
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. Setup Infrastructure: Create a Group & Policy controlled by this actor
		// This allows us to "become" the council for this transaction.
		members := []group.MemberRequest{
			{Address: simAccount.Address.String(), Weight: "1", Metadata: "sim member"},
		}

		groupRes, err := gk.CreateGroup(ctx, &group.MsgCreateGroup{
			Admin:   simAccount.Address.String(),
			Members: members,
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to create group"), nil, err
		}

		decisionPolicy := group.NewThresholdDecisionPolicy(
			"1", // Threshold 1 means our single member can pass proposals
			time.Hour*24,
			time.Duration(0), // 0 execution period allows immediate execution
		)

		createPolicyMsg, err := group.NewMsgCreateGroupPolicy(simAccount.Address, groupRes.GroupId, "standard", decisionPolicy)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create policy msg"), nil, err
		}

		policyRes, err := gk.CreateGroupPolicy(ctx, createPolicyMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create sim policy"), nil, err
		}

		// C. Update commons module Params with the new Policy Address
		// We retrieve the current params, update the address, and set them back.
		commonsParams, err := ck.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to get commons params"), nil, err
		}

		commonsParams.CommonsCouncilAddress = policyRes.Address

		if err := ck.SetParams(ctx, commonsParams); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to set commons params"), nil, err
		}

		// 3. Update Params: Make this new group the official Council
		params := k.GetParams(ctx)
		params.CouncilGroupId = groupRes.GroupId
		if err := k.SetParams(ctx, params); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to update params"), nil, err
		}

		// 4. Setup Target: Find or Create a Dispute
		// To be safe, we inject a dispute so we don't rely on random previous ops
		disputeName := simtypes.RandStringOfLength(r, 10)
		disputeRecord := types.Dispute{
			Name:     disputeName,
			Claimant: simAccount.Address.String(),
		}
		if err := k.SetDispute(ctx, disputeRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to set dispute"), nil, err
		}

		// 5. Construct the Inner Message (MsgResolveDispute)
		// The Authority MUST be the Group Policy Address we just created
		newOwner, _ := simtypes.RandomAcc(r, accs)

		resolveMsg := &types.MsgResolveDispute{
			Authority: policyRes.Address,
			Name:      disputeName,
			NewOwner:  newOwner.Address.String(),
		}

		// 6. Use Any to wrap the message (standard Cosmos SDK pattern)
		anyMsg, err := codectypes.NewAnyWithValue(resolveMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to wrap any"), nil, err
		}

		proposalMsg := &group.MsgSubmitProposal{
			GroupPolicyAddress: policyRes.Address,
			Proposers:          []string{simAccount.Address.String()},
			Messages:           []*codectypes.Any{anyMsg},
			Metadata:           "resolving dispute simulation",
			Exec:               group.Exec_EXEC_TRY, // Execute immediately!
		}

		// 7. Execute Transaction
		// We are sending a MsgSubmitProposal, but the "Op" is technically regarding x/name resolution
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             proposalMsg, // We send the Proposal message
			CoinsSpentInMsg: nil,
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
