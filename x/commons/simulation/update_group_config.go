package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
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
			targetGroup types.Group
			targetName  string
			found       bool
		)

		simAccount, _ = simtypes.RandomAcc(r, accs)

		// 1. FIND CANDIDATE GROUP
		// Find a group where simAccount is the PARENT (Authorized to update config)
		err := k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
			if g.ParentPolicyAddress == simAccount.Address.String() {
				// SKIP INVALID GROUPS
				if g.MinMembers == 0 {
					return false, nil
				}
				targetGroup = g
				targetName = name
				found = true
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupConfig{}), "error walking groups"), nil, err
		}

		// 2. FALLBACK: CREATE GROUP
		if !found {
			targetName = "sim-config-group-" + simtypes.RandStringOfLength(r, 5)
			policyAddr := "sim-config-policy-" + simtypes.RandStringOfLength(r, 10)

			targetGroup = types.Group{
				GroupId:             uint64(simtypes.RandIntBetween(r, 1, 1000)),
				PolicyAddress:       policyAddr,
				ParentPolicyAddress: simAccount.Address.String(), // Key: simAccount is Parent
				MinMembers:          1,
				MaxMembers:          5,
				TermDuration:        86400,
				FutarchyEnabled:     true,
			}
			if err := k.Groups.Set(ctx, targetName, targetGroup); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupConfig{}), "failed to set group"), nil, err
			}
			if err := k.PolicyToName.Set(ctx, policyAddr, targetName); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupConfig{}), "failed to set policy index"), nil, err
			}

			// Add initial member
			if err := k.AddMember(ctx, targetName, types.Member{
				Address: simAccount.Address.String(),
				Weight:  "1",
				AddedAt: ctx.BlockTime().Unix(),
			}); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateGroupConfig{}), "failed to add member"), nil, err
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

				strVal := fmt.Sprintf("0.%d", simtypes.RandIntBetween(r, 51, 99))
				dec := math.LegacyMustNewDecFromStr(strVal)
				msg.VoteThreshold = &dec
			} else {
				// Threshold
				msg.PolicyType = keeper.PolicyTypeThreshold

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
