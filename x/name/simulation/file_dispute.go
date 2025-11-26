package simulation

import (
	"math/rand"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commonskeeper "sparkdream/x/commons/keeper"
	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgFileDispute(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	ck commonskeeper.Keeper,
	gk groupkeeper.Keeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Get Params early
		params := k.GetParams(ctx)
		disputeFee := params.DisputeFee

		// 2. Define Fee Requirements (Dispute Fee + Gas Buffer)
		// We need a buffer because GenAndDeliverTxWithRandFees deducts gas BEFORE our handler runs.
		// If the account has exactly 'disputeFee', it will fail after gas is taken.
		denom := disputeFee.Denom
		if denom == "" {
			denom = "uspark" // Fallback if fee is zero/empty
		}

		// Add a buffer of ~1 token (or 1M utokens) for gas safety
		feeBuffer := sdk.NewInt64Coin(denom, 1000000)

		var requiredBalance sdk.Coins
		if !disputeFee.IsZero() {
			requiredBalance = sdk.NewCoins(disputeFee).Add(feeBuffer)
		} else {
			requiredBalance = sdk.NewCoins(feeBuffer)
		}

		// 3. Find a Solvent Account
		var simAccount simtypes.Account
		var found bool

		// Shuffle to avoid bias
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "no account with sufficient funds (fee + gas buffer)"), nil, nil
		}

		// --- PRE-REQUISITE SETUP (Create Council & Policy) ---

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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create sim group"), nil, err
		}

		// B. Create "standard" Decision Policy
		// The MsgServer looks for a policy with metadata "standard" to identify the Council Address.
		decisionPolicy := group.NewThresholdDecisionPolicy(
			"1",              // threshold
			time.Hour*24,     // voting period
			time.Duration(0), // min execution period
		)

		createPolicyMsg, err := group.NewMsgCreateGroupPolicy(simAccount.Address, groupRes.GroupId, "standard", decisionPolicy)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create policy msg"), nil, err
		}

		policyRes, err := gk.CreateGroupPolicy(ctx, createPolicyMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to create sim policy"), nil, err
		}

		// C. Update commons module Params
		commonsParams, err := ck.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to get commons params"), nil, err
		}
		commonsParams.CommonsCouncilAddress = policyRes.Address
		if err := ck.SetParams(ctx, commonsParams); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to set commons params"), nil, err
		}

		// D. Update x/name Params
		params.CouncilGroupId = groupRes.GroupId
		if err := k.SetParams(ctx, params); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to update name params"), nil, err
		}

		// --- EXECUTE DISPUTE ---

		// 4. Create a Name to Dispute
		targetName := strings.ToLower(simtypes.RandStringOfLength(r, 10))
		targetData := simtypes.RandStringOfLength(r, 20)
		targetOwner, _ := simtypes.RandomAcc(r, accs)

		record := types.NameRecord{
			Name:  targetName,
			Owner: targetOwner.Address.String(),
			Data:  targetData,
		}

		// Inject Name Record into Keeper
		if err := k.SetName(ctx, record); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to set name record"), nil, err
		}
		if err := k.AddNameToOwner(ctx, targetOwner.Address, targetName); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "failed to add name to owner"), nil, err
		}

		// Sanity Check
		_, disputeFound := k.GetDispute(ctx, targetName)
		if disputeFound {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFileDispute{}), "dispute already exists"), nil, nil
		}

		msg := &types.MsgFileDispute{
			Authority: simAccount.Address.String(),
			Name:      targetName,
		}

		// 5. Deliver Transaction
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(disputeFee), // Record that the dispute fee is "spent" by logic
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		// GenAndDeliverTxWithRandFees handles gas deduction automatically.
		// Our "requiredBalance" check ensures the user survives this deduction.
		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
