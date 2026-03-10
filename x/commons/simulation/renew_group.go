package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
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
			targetGroup types.Group
			targetName  string
			found       bool
		)

		// 1. SELECT SIM ACCOUNT
		simAccount, _ = simtypes.RandomAcc(r, accs)

		// 2. FIND CANDIDATE GROUP (Self-Renewal)
		err := k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
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
		if !found {
			targetName = "sim-group-" + simtypes.RandStringOfLength(r, 5)
			policyAddr := "sim-renew-policy-" + simtypes.RandStringOfLength(r, 10)

			targetGroup = types.Group{
				GroupId:               uint64(simtypes.RandIntBetween(r, 1, 1000)),
				PolicyAddress:         policyAddr,
				ParentPolicyAddress:   simAccount.Address.String(),
				MinMembers:            1,
				MaxMembers:            5,
				TermDuration:          86400,
				CurrentTermExpiration: ctx.BlockTime().Unix() + 86400,
				FutarchyEnabled:       false,
			}
			if err := k.Groups.Set(ctx, targetName, targetGroup); err != nil {
				dummyMsg := &types.MsgRenewGroup{}
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "failed to set group"), nil, err
			}
			if err := k.PolicyToName.Set(ctx, policyAddr, targetName); err != nil {
				dummyMsg := &types.MsgRenewGroup{}
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "failed to set policy index"), nil, err
			}

			// Add initial member
			if err := k.AddMember(ctx, targetName, types.Member{
				Address: simAccount.Address.String(),
				Weight:  "1",
				AddedAt: ctx.BlockTime().Unix(),
			}); err != nil {
				dummyMsg := &types.MsgRenewGroup{}
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "failed to add member"), nil, err
			}
		}

		// 4. TIME TRAVEL (State Injection)
		targetGroup.CurrentTermExpiration = ctx.BlockTime().Unix() - 1
		if err := k.Groups.Set(ctx, targetName, targetGroup); err != nil {
			dummyMsg := &types.MsgRenewGroup{}
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "failed to expire group term"), nil, err
		}

		// 5. GENERATE NEW MEMBERS
		effectiveMax := targetGroup.MaxMembers
		if uint64(len(accs)) < effectiveMax {
			effectiveMax = uint64(len(accs))
		}

		if effectiveMax < targetGroup.MinMembers {
			dummyMsg := &types.MsgRenewGroup{}
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(dummyMsg), "not enough sim accounts for min members"), nil, nil
		}

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
