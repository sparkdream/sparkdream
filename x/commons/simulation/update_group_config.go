package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
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

func SimulateMsgUpdateGroupConfig(
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

		simAccount, _ = simtypes.RandomAcc(r, accs)
		moduleAddr := k.GetModuleAddress().String()

		// 1. FIND CANDIDATE GROUP
		// Find a group where simAccount is the PARENT (Authorized to update config)
		err := k.ExtendedGroup.Walk(ctx, nil, func(name string, g types.ExtendedGroup) (bool, error) {
			if g.ParentPolicyAddress == simAccount.Address.String() {
				// SKIP INVALID GROUPS
				if g.MinMembers == 0 {
					return false, nil
				}

				// Verify backing x/group exists
				_, err := k.GetGroupKeeper().GroupInfo(ctx, &group.QueryGroupInfoRequest{GroupId: g.GroupId})
				if err == nil {
					targetGroup = g
					targetName = name
					found = true
					return true, nil
				}
			}
			return false, nil
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupConfig{}), "error walking groups"), nil, err
		}

		// 2. FALLBACK: CREATE GROUP
		if !found {
			// A. Create x/group (Module is Admin)
			groupRes, err := k.GetGroupKeeper().CreateGroup(ctx, &group.MsgCreateGroup{
				Admin:    moduleAddr,
				Members:  []group.MemberRequest{{Address: simAccount.Address.String(), Weight: "1", Metadata: "sim-member"}},
				Metadata: "sim-config-update-group",
			})
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupConfig{}), "failed to create x/group"), nil, nil
			}

			// B. Create Policy
			decisionPolicy := group.NewThresholdDecisionPolicy("1", 3600, 0)
			policyAny, _ := codectypes.NewAnyWithValue(decisionPolicy)
			policyRes, err := k.GetGroupKeeper().CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
				Admin:          moduleAddr,
				GroupId:        groupRes.GroupId,
				DecisionPolicy: policyAny,
			})
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupConfig{}), "failed to create policy"), nil, nil
			}

			// C. Register ExtendedGroup
			targetName = "sim_config_group_" + simtypes.RandStringOfLength(r, 5)
			targetGroup = types.ExtendedGroup{
				GroupId:             groupRes.GroupId,
				PolicyAddress:       policyRes.Address,
				ParentPolicyAddress: simAccount.Address.String(), // Key: simAccount is Parent
				MinMembers:          1,
				MaxMembers:          5,
				TermDuration:        86400,
				FutarchyEnabled:     true,
			}
			if err := k.ExtendedGroup.Set(ctx, targetName, targetGroup); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupConfig{}), "failed to set extended group"), nil, err
			}
		}

		// 3. GENERATE RANDOM UPDATES
		msg := &types.MsgUpdateGroupConfig{
			Authority: simAccount.Address.String(),
			GroupName: targetName,
		}

		// A. Update Member Bounds (50% chance OR if group is invalid)
		if r.Intn(2) == 0 || targetGroup.MinMembers == 0 {
			newMin := uint64(simtypes.RandIntBetween(r, 1, 5))
			newMax := uint64(simtypes.RandIntBetween(r, int(newMin), 10))
			msg.MinMembers = newMin
			msg.MaxMembers = newMax
		}

		// B. Update Spend Limit (50% chance)
		if r.Intn(2) == 0 {
			// Generate string -> Convert to Dec -> Take Address
			strVal := fmt.Sprintf("%dstake", simtypes.RandIntBetween(r, 100, 10000))
			dec := math.LegacyMustNewDecFromStr(strVal)
			msg.VoteThreshold = &dec
		}

		// C. Update Cooldown (50% chance)
		if r.Intn(2) == 0 {
			msg.UpdateCooldown = int64(simtypes.RandIntBetween(r, 0, 86400))
		}

		// D. Update Futarchy (50% chance)
		if r.Intn(2) == 0 {
			val := r.Intn(2) == 0 // true or false
			msg.FutarchyEnabled = &types.BoolValue{Value: val}
		}

		// E. Update Policy (30% chance - less frequent as it's heavier)
		if r.Intn(10) < 3 {
			if r.Intn(2) == 0 {
				// Percentage
				msg.PolicyType = keeper.PolicyTypePercentage

				// Generate string -> Convert to Dec -> Take Address
				strVal := fmt.Sprintf("0.%d", simtypes.RandIntBetween(r, 51, 99))
				dec := math.LegacyMustNewDecFromStr(strVal)
				msg.VoteThreshold = &dec
			} else {
				// Threshold
				msg.PolicyType = keeper.PolicyTypeThreshold

				// Generate int -> Convert to Dec -> Take Address
				dec := math.LegacyNewDec(int64(simtypes.RandIntBetween(r, 1, 5)))
				msg.VoteThreshold = &dec
			}
			msg.VotingPeriod = int64(simtypes.RandIntBetween(r, 3600, 172800))
			msg.MinExecutionPeriod = 0
		}

		// F. Update Term Duration (50% chance)
		if r.Intn(2) == 0 {
			msg.TermDuration = int64(simtypes.RandIntBetween(r, 86400, 86400*30))
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
