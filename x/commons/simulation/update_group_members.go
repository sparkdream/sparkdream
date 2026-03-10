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

func SimulateMsgUpdateGroupMembers(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var (
			simAccount  simtypes.Account
			childGroup  types.Group
			childName   string
			parentGroup types.Group
			parentName  string
			found       bool
		)

		simAccount, _ = simtypes.RandomAcc(r, accs)

		// 1. FIND OR CREATE HIERARCHY
		// Try to find a Child Group controlled by this simAccount that is ALSO the Electoral Authority
		err := k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
			if g.PolicyAddress == simAccount.Address.String() && g.ParentPolicyAddress != "" {
				// Verify parent exists
				pName, pGroup, pFound := getGroupByPolicy(ctx, k, g.ParentPolicyAddress)
				if pFound {
					// STRICT AUTH CHECK: The parent MUST have delegated authority to this child
					if pGroup.ElectoralPolicyAddress == g.PolicyAddress {
						childGroup = g
						childName = name
						parentGroup = pGroup
						parentName = pName
						found = true
						return true, nil
					}
				}
			}
			return false, nil
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "error walking groups"), nil, err
		}

		// 2. FALLBACK: CREATE HIERARCHY
		if !found {
			// Pre-calculate Child Policy Address (we use the Sim Account)
			childPolicyAddr := simAccount.Address.String()

			// A. Create Parent state via native collections
			initialMember, _ := simtypes.RandomAcc(r, accs)
			parentName = "sim-parent-" + simtypes.RandStringOfLength(r, 5)
			parentPolicyAddr := "sim-parent-policy-" + simtypes.RandStringOfLength(r, 10)

			parentGroup = types.Group{
				GroupId:                1,
				PolicyAddress:          parentPolicyAddr,
				MinMembers:             1,
				MaxMembers:             10,
				ElectoralPolicyAddress: childPolicyAddr,
			}
			if err := k.Groups.Set(ctx, parentName, parentGroup); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to set parent group"), nil, err
			}
			if err := k.PolicyToName.Set(ctx, parentGroup.PolicyAddress, parentName); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to set parent index"), nil, err
			}

			// Add initial member
			if err := k.AddMember(ctx, parentName, types.Member{
				Address: initialMember.Address.String(),
				Weight:  "1",
				AddedAt: ctx.BlockTime().Unix(),
			}); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to add parent member"), nil, err
			}

			// B. Register Child Group (The Authority)
			childName = "sim-child-" + simtypes.RandStringOfLength(r, 5)
			childGroup = types.Group{
				PolicyAddress:       childPolicyAddr,
				ParentPolicyAddress: parentGroup.PolicyAddress,
				UpdateCooldown:      3600,
				LastParentUpdate:    0, // Ready immediately
			}
			if err := k.Groups.Set(ctx, childName, childGroup); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to set child group"), nil, err
			}
			if err := k.PolicyToName.Set(ctx, childGroup.PolicyAddress, childName); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to set child index"), nil, err
			}
		}

		// 3. RESET COOLDOWN (Time Travel)
		childGroup.LastParentUpdate = ctx.BlockTime().Unix() - int64(childGroup.UpdateCooldown) - 1
		if err := k.Groups.Set(ctx, childName, childGroup); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to reset cooldown"), nil, err
		}

		// 4. ANALYZE CURRENT PARENT STATE
		members, err := k.GetCouncilMembers(ctx, parentName)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to fetch parent members"), nil, err
		}

		currentMembers := make(map[string]bool)
		for _, m := range members {
			currentMembers[m.Address] = true
		}
		currentCount := uint64(len(currentMembers))

		// 5. DETERMINE TARGET STATE
		min := int(parentGroup.MinMembers)
		max := int(parentGroup.MaxMembers)
		if max > len(accs) {
			max = len(accs)
		}
		if min > max {
			min = max
		}

		targetCount := uint64(min)
		if max > min {
			targetCount = uint64(simtypes.RandIntBetween(r, min, max+1))
		}

		var membersToAdd []string
		var weightsToAdd []string
		var membersToRemove []string

		// 6. CALCULATE DELTA
		if targetCount > currentCount {
			needed := int(targetCount - currentCount)
			candidates := []string{}
			for _, acc := range accs {
				if !currentMembers[acc.Address.String()] {
					candidates = append(candidates, acc.Address.String())
				}
			}

			if len(candidates) >= needed {
				perm := r.Perm(len(candidates))
				for i := 0; i < needed; i++ {
					membersToAdd = append(membersToAdd, candidates[perm[i]])
					weightsToAdd = append(weightsToAdd, "1")
				}
			}
		} else if targetCount < currentCount {
			needed := int(currentCount - targetCount)
			candidates := []string{}
			for addr := range currentMembers {
				candidates = append(candidates, addr)
			}

			if len(candidates) >= needed {
				perm := r.Perm(len(candidates))
				for i := 0; i < needed; i++ {
					membersToRemove = append(membersToRemove, candidates[perm[i]])
				}
			}
		} else {
			// Target == Current. Perform a SWAP (Remove 1, Add 1)
			if currentCount > 0 && currentCount < uint64(len(accs)) {
				for addr := range currentMembers {
					membersToRemove = append(membersToRemove, addr)
					break
				}
				for _, acc := range accs {
					if !currentMembers[acc.Address.String()] {
						membersToAdd = append(membersToAdd, acc.Address.String())
						weightsToAdd = append(weightsToAdd, "1")
						break
					}
				}
			}
		}

		// 7. CONSTRUCT MESSAGE
		msg := &types.MsgUpdateGroupMembers{
			Authority:          simAccount.Address.String(),
			GroupPolicyAddress: parentGroup.PolicyAddress,
			MembersToAdd:       membersToAdd,
			WeightsToAdd:       weightsToAdd,
			MembersToRemove:    membersToRemove,
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

// Helper duplicated from keeper to find groups by policy in simulation context
func getGroupByPolicy(ctx sdk.Context, k keeper.Keeper, policyAddr string) (string, types.Group, bool) {
	var foundGroup types.Group
	var foundName string
	found := false

	k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
		if g.PolicyAddress == policyAddr {
			foundGroup = g
			foundName = name
			found = true
			return true, nil
		}
		return false, nil
	})

	return foundName, foundGroup, found
}
