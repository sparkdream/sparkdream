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
			childGroup  types.ExtendedGroup
			childName   string
			parentGroup types.ExtendedGroup
			found       bool
		)

		simAccount, _ = simtypes.RandomAcc(r, accs)
		moduleAddr := k.GetModuleAddress().String()

		// 1. FIND OR CREATE HIERARCHY
		// Try to find a Child Group controlled by this simAccount that is ALSO the Electoral Authority
		err := k.ExtendedGroup.Walk(ctx, nil, func(name string, g types.ExtendedGroup) (bool, error) {
			if g.PolicyAddress == simAccount.Address.String() && g.ParentPolicyAddress != "" {
				// Verify parent exists
				pName, pGroup, pFound := getExtendedGroupByPolicy(ctx, k, g.ParentPolicyAddress)
				if pFound {
					// STRICT AUTH CHECK: The parent MUST have delegated authority to this child
					if pGroup.ElectoralPolicyAddress == g.PolicyAddress {
						childGroup = g
						childName = name
						parentGroup = pGroup

						// Verify backing x/group exists for parent (vital for GroupMembers query)
						_, err := k.GetGroupKeeper().GroupInfo(ctx, &group.QueryGroupInfoRequest{GroupId: pGroup.GroupId})
						if err == nil {
							found = true
							_ = pName // unused
							return true, nil
						}
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

			// A. Create Parent x/group (Must be Admin'd by Module Account)
			// Add a random initial member to satisfy potential MinMembers > 0
			initialMember, _ := simtypes.RandomAcc(r, accs)
			pGroupRes, err := k.GetGroupKeeper().CreateGroup(ctx, &group.MsgCreateGroup{
				Admin:    moduleAddr,
				Members:  []group.MemberRequest{{Address: initialMember.Address.String(), Weight: "1", Metadata: "sim-parent-member"}},
				Metadata: "sim-parent-group",
			})
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to create parent x/group"), nil, nil
			}

			// B. Create Parent Policy
			decisionPolicy := group.NewThresholdDecisionPolicy("1", 3600, 0)
			policyAny, _ := codectypes.NewAnyWithValue(decisionPolicy)
			pPolicyRes, err := k.GetGroupKeeper().CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
				Admin:          moduleAddr,
				GroupId:        pGroupRes.GroupId,
				DecisionPolicy: policyAny,
			})
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to create parent policy"), nil, nil
			}

			// C. Register Parent ExtendedGroup
			parentGroup = types.ExtendedGroup{
				GroupId:                pGroupRes.GroupId,
				PolicyAddress:          pPolicyRes.Address,
				MinMembers:             1,
				MaxMembers:             10,
				ElectoralPolicyAddress: childPolicyAddr,
			}

			// Generate name explicitly so we can set the index
			parentName := "sim_parent_" + simtypes.RandStringOfLength(r, 5)
			if err := k.ExtendedGroup.Set(ctx, parentName, parentGroup); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to set parent extended group"), nil, err
			}
			// Set the PolicyToName Index
			if err := k.PolicyToName.Set(ctx, parentGroup.PolicyAddress, parentName); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to set parent index"), nil, err
			}

			// D. Register Child ExtendedGroup (The Authority)
			childName = "sim_child_" + simtypes.RandStringOfLength(r, 5)
			childGroup = types.ExtendedGroup{
				PolicyAddress:       childPolicyAddr,
				ParentPolicyAddress: parentGroup.PolicyAddress,
				UpdateCooldown:      3600,
				LastParentUpdate:    0, // Ready immediately
			}
			if err := k.ExtendedGroup.Set(ctx, childName, childGroup); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to set child extended group"), nil, err
			}
			// Set the PolicyToName Index
			if err := k.PolicyToName.Set(ctx, childGroup.PolicyAddress, childName); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to set child index"), nil, err
			}
		}

		// 3. RESET COOLDOWN (Time Travel)
		// Set the LastParentUpdate to the PAST relative to the current block time.
		childGroup.LastParentUpdate = ctx.BlockTime().Unix() - int64(childGroup.UpdateCooldown) - 1

		if err := k.ExtendedGroup.Set(ctx, childName, childGroup); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to reset cooldown"), nil, err
		}

		// 4. ANALYZE CURRENT PARENT STATE
		// Fetch current members to determine valid adds/removes
		resp, err := k.GetGroupKeeper().GroupMembers(ctx, &group.QueryGroupMembersRequest{GroupId: parentGroup.GroupId})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}), "failed to fetch parent members"), nil, err
		}

		currentMembers := make(map[string]bool)
		for _, m := range resp.Members {
			currentMembers[m.Member.Address] = true
		}
		currentCount := uint64(len(currentMembers))

		// 5. DETERMINE TARGET STATE
		// Pick a target size between [Min, Max]
		min := int(parentGroup.MinMembers)
		max := int(parentGroup.MaxMembers)
		// Safety cap against sim account limits
		if max > len(accs) {
			max = len(accs)
		}
		if min > max {
			min = max
		}

		// Random target count
		targetCount := uint64(min)
		if max > min {
			targetCount = uint64(simtypes.RandIntBetween(r, min, max+1))
		}

		var membersToAdd []string
		var weightsToAdd []string
		var membersToRemove []string

		// 6. CALCULATE DELTA
		if targetCount > currentCount {
			// NEED TO ADD
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
			// NEED TO REMOVE
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
func getExtendedGroupByPolicy(ctx sdk.Context, k keeper.Keeper, policyAddr string) (string, types.ExtendedGroup, bool) {
	var foundGroup types.ExtendedGroup
	var foundName string
	found := false

	k.ExtendedGroup.Walk(ctx, nil, func(name string, g types.ExtendedGroup) (bool, error) {
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
