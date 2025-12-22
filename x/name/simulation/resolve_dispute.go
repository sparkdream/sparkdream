package simulation

import (
	"math/rand"
	"time"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgResolveDispute(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	ck types.CommonsKeeper,
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
		members := []group.MemberRequest{
			{Address: simAccount.Address.String(), Weight: "1", Metadata: "sim member"},
		}

		groupRes, err := gk.CreateGroup(ctx, &group.MsgCreateGroup{
			Admin:   simAccount.Address.String(),
			Members: members,
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to create group"), nil, nil
		}

		decisionPolicy := group.NewThresholdDecisionPolicy(
			"1", // Threshold 1 means our single member can pass proposals
			time.Hour*24,
			time.Duration(0), // 0 execution period allows immediate execution
		)

		createPolicyMsg, err := group.NewMsgCreateGroupPolicy(simAccount.Address, groupRes.GroupId, "standard", decisionPolicy)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to create policy msg"), nil, nil
		}

		policyRes, err := gk.CreateGroupPolicy(ctx, createPolicyMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to create sim policy"), nil, nil
		}

		// 3. INJECT PERMISSIONS (RBAC Setup)
		// We explicitly grant the policy permission to execute MsgResolveDispute.
		// This replaces the old check against CommonsCouncilAddress.
		perms := commonstypes.PolicyPermissions{
			PolicyAddress: policyRes.Address,
			AllowedMessages: []string{
				"/sparkdream.name.v1.MsgResolveDispute",
			},
		}
		if err := ck.SetPolicyPermissions(ctx, policyRes.Address, perms); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to inject permissions"), nil, nil
		}

		// 4. Setup Target: Find or Create a Dispute
		disputeName := simtypes.RandStringOfLength(r, 10)

		// Create Name Record First (Required for resolution transfer)
		nameRecord := types.NameRecord{
			Name:  disputeName,
			Owner: simAccount.Address.String(), // Current owner doesn't matter much for this sim
			Data:  "disputed data",
		}
		if err := k.SetName(ctx, nameRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to set name record"), nil, nil
		}

		// Create Dispute Record
		disputeRecord := types.Dispute{
			Name:     disputeName,
			Claimant: simAccount.Address.String(),
		}
		if err := k.SetDispute(ctx, disputeRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to set dispute"), nil, nil
		}

		// 6. Construct the Inner Message (MsgResolveDispute)
		newOwner, _ := simtypes.RandomAcc(r, accs)

		resolveMsg := &types.MsgResolveDispute{
			Authority: policyRes.Address,
			Name:      disputeName,
			NewOwner:  newOwner.Address.String(),
		}

		// 7. Wrap and Execute Proposal
		anyMsg, err := codectypes.NewAnyWithValue(resolveMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveDispute{}), "failed to wrap any"), nil, nil
		}

		proposalMsg := &group.MsgSubmitProposal{
			GroupPolicyAddress: policyRes.Address,
			Proposers:          []string{simAccount.Address.String()},
			Messages:           []*codectypes.Any{anyMsg},
			Metadata:           "resolving dispute simulation",
			Exec:               group.Exec_EXEC_TRY, // Execute immediately!
		}

		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             proposalMsg,
			CoinsSpentInMsg: nil, // Proposals technically have fees, but we simulate without for simplicity here
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		// Define explicit high fees to satisfy the AnteHandler check (5M uspark)
		// Random fees are usually too low for the x/commons spam protection.
		fees := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5000000)))

		// Use GenAndDeliverTx (explicit fees) instead of GenAndDeliverTxWithRandFees
		return simulation.GenAndDeliverTx(opMsg, fees)
	}
}
