package simulation

import (
	"math/rand"
	"strings"
	"time"

	commonstypes "sparkdream/x/commons/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgFileDispute(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	ck types.CommonsKeeper,
	gk groupkeeper.Keeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Find a Solvent Account (gas buffer only — DREAM staking is handled by x/rep)
		feeBuffer := sdk.NewInt64Coin("uspark", 1000000)
		requiredBalance := sdk.NewCoins(feeBuffer)

		var simAccount simtypes.Account
		var found bool
		r.Shuffle(len(accs), func(i, j int) { accs[i], accs[j] = accs[j], accs[i] })

		for _, acc := range accs {
			spendable := bk.SpendableCoins(ctx, acc.Address)
			if spendable.IsAllGTE(requiredBalance) {
				simAccount = acc
				found = true
				break
			}
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "no account with sufficient funds"), nil, nil
		}

		// --- PRE-REQUISITE SETUP ---

		// A. Create a Group
		members := []group.MemberRequest{
			{
				Address:  simAccount.Address.String(),
				Weight:   "1",
				Metadata: "simulation member",
			},
		}

		createGroupMsg := &group.MsgCreateGroup{
			Admin:    simAccount.Address.String(),
			Members:  members,
			Metadata: "simulation council",
		}
		groupRes, err := gk.CreateGroup(ctx, createGroupMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create sim group"), nil, nil
		}

		// B. Create "standard" Decision Policy
		decisionPolicy := group.NewThresholdDecisionPolicy(
			"1",              // threshold
			time.Hour*24,     // voting period
			time.Duration(0), // min execution period
		)

		createPolicyMsg, err := group.NewMsgCreateGroupPolicy(simAccount.Address, groupRes.GroupId, "standard", decisionPolicy)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create policy msg"), nil, nil
		}

		policyRes, err := gk.CreateGroupPolicy(ctx, createPolicyMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create sim policy"), nil, nil
		}

		// C. REGISTER EXTENDED GROUP IN COMMONS KEEPER
		simExtendedGroup := commonstypes.ExtendedGroup{
			GroupId:       groupRes.GroupId,
			PolicyAddress: policyRes.Address,
		}
		if err := ck.SetExtendedGroup(ctx, "Commons Council", simExtendedGroup); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to register sim extended group"), nil, nil
		}

		// D. INJECT PERMISSIONS (RBAC Setup)
		perms := commonstypes.PolicyPermissions{
			PolicyAddress: policyRes.Address,
			AllowedMessages: []string{
				"/sparkdream.name.v1.MsgResolveDispute",
				"/sparkdream.commons.v1.MsgSpendFromCommons",
			},
		}
		if err := ck.SetPolicyPermissions(ctx, policyRes.Address, perms); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to inject permissions"), nil, nil
		}

		// --- EXECUTE DISPUTE ---

		// 4. Create a Name to Dispute
		targetName := strings.ToLower(simtypes.RandStringOfLength(r, 10))
		targetData := simtypes.RandStringOfLength(r, 20)
		targetOwner, _ := simtypes.RandomAcc(r, accs)

		// Collision check
		_, foundName := k.GetName(ctx, targetName)
		if foundName {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "name already exists"), nil, nil
		}

		record := types.NameRecord{
			Name:  targetName,
			Owner: targetOwner.Address.String(),
			Data:  targetData,
		}

		if err := k.SetName(ctx, record); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to set name record"), nil, nil
		}
		if err := k.AddNameToOwner(ctx, targetOwner.Address, targetName); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to add name to owner"), nil, nil
		}

		// Sanity Check
		_, disputeFound := k.GetDispute(ctx, targetName)
		if disputeFound {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "dispute already exists"), nil, nil
		}

		msg := &types.MsgFileDispute{
			Authority: simAccount.Address.String(),
			Name:      targetName,
			Reason:    "simulation dispute reason",
		}

		// 5. Deliver Transaction (no SPARK coins spent — DREAM staking is internal)
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
