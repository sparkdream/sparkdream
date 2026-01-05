package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/group"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func SimulateMsgRenewGroup(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var (
			simAccount  simtypes.Account
			targetGroup types.ExtendedGroup
			targetName  string
			found       bool
		)

		// 1. SELECT SIM ACCOUNT
		simAccount, _ = simtypes.RandomAcc(r, accs)

		// 2. FIND CANDIDATE GROUP (Self-Renewal)
		// Look for a group where the ParentPolicyAddress matches our simAccount.
		err := k.ExtendedGroup.Walk(ctx, nil, func(name string, g types.ExtendedGroup) (bool, error) {
			if g.ParentPolicyAddress == simAccount.Address.String() {
				targetGroup = g
				targetName = name
				found = true
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			dummyMsg := &types.MsgRenewGroup{}
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "error walking groups"), nil, err
		}

		// 3. FALLBACK: CREATE IF NOT FOUND
		// If this account doesn't own a group, we create one for it dynamically.
		if !found {
			// The underlying group MUST be admin'd by the x/commons module account
			// so that the MsgRenewGroup handler can update it later.
			moduleAddr := k.GetModuleAddress().String()

			// A. Create x/group Group
			// We use moduleAddr as Admin
			groupRes, err := k.GetGroupKeeper().CreateGroup(ctx, &group.MsgCreateGroup{
				Admin:    moduleAddr,
				Members:  []group.MemberRequest{{Address: simAccount.Address.String(), Weight: "1", Metadata: "sim-member"}},
				Metadata: "sim-auto-created",
			})
			if err != nil {
				dummyMsg := &types.MsgRenewGroup{}
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "failed to create backing x/group"), nil, nil
			}

			// B. Create x/group Policy
			decisionPolicy := group.NewThresholdDecisionPolicy("1", 3600, 0)
			policyAny, _ := codectypes.NewAnyWithValue(decisionPolicy)

			// We use moduleAddr as Admin here too
			policyRes, err := k.GetGroupKeeper().CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
				Admin:          moduleAddr,
				GroupId:        groupRes.GroupId,
				DecisionPolicy: policyAny,
			})
			if err != nil {
				dummyMsg := &types.MsgRenewGroup{}
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "failed to create backing x/group policy"), nil, nil
			}

			// C. Create ExtendedGroup
			targetName = "sim_group_" + simtypes.RandStringOfLength(r, 5)
			targetGroup = types.ExtendedGroup{
				GroupId:       groupRes.GroupId,
				PolicyAddress: policyRes.Address,
				// ParentPolicyAddress is the Sim Account.
				// This allows the Sim Account to authorize the renewal request,
				// even though the Module Account owns the underlying group.
				ParentPolicyAddress:   simAccount.Address.String(),
				MinMembers:            1,
				MaxMembers:            5,
				TermDuration:          86400,
				CurrentTermExpiration: ctx.BlockTime().Unix() + 86400,
				FutarchyEnabled:       false,
			}
			if err := k.ExtendedGroup.Set(ctx, targetName, targetGroup); err != nil {
				dummyMsg := &types.MsgRenewGroup{}
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "failed to set extended group"), nil, err
			}
		}

		// 4. TIME TRAVEL (State Injection)
		// Force expiration to be in the past so validation passes.
		targetGroup.CurrentTermExpiration = ctx.BlockTime().Unix() - 1
		if err := k.ExtendedGroup.Set(ctx, targetName, targetGroup); err != nil {
			dummyMsg := &types.MsgRenewGroup{}
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "failed to expire group term"), nil, err
		}

		// 5. GENERATE NEW MEMBERS
		// Determine valid member count [MinMembers, MaxMembers]
		effectiveMax := targetGroup.MaxMembers
		if uint64(len(accs)) < effectiveMax {
			effectiveMax = uint64(len(accs))
		}

		if effectiveMax < targetGroup.MinMembers {
			dummyMsg := &types.MsgRenewGroup{}
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "not enough sim accounts for min members"), nil, nil
		}

		// Calculate count: [min, max]
		var newCount uint64
		if effectiveMax == targetGroup.MinMembers {
			newCount = targetGroup.MinMembers
		} else {
			minInt := int(targetGroup.MinMembers)
			maxInt := int(effectiveMax)
			if maxInt > minInt {
				newCount = uint64(simtypes.RandIntBetween(r, minInt, maxInt+1))
			} else {
				newCount = uint64(minInt)
			}
		}

		if newCount == 0 {
			dummyMsg := &types.MsgRenewGroup{}
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "new member count is 0"), nil, nil
		}

		// Select unique members
		perm := r.Perm(len(accs))
		var newMembers []string
		var newWeights []string

		for i := 0; i < int(newCount); i++ {
			acc := accs[perm[i]]
			newMembers = append(newMembers, acc.Address.String())
			newWeights = append(newWeights, "1")
		}

		// 6. BUILD MESSAGE
		msg := &types.MsgRenewGroup{
			Authority:        simAccount.Address.String(),
			GroupName:        targetName,
			NewMembers:       newMembers,
			NewMemberWeights: newWeights,
		}

		txCtx := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			Context:         ctx,
			SimAccount:      simAccount,
			ModuleName:      types.ModuleName,
			CoinsSpentInMsg: sdk.NewCoins(),
			AccountKeeper:   ak,
			Bankkeeper:      bk,
		}

		return simulation.GenAndDeliverTxWithRandFees(txCtx)
	}
}
