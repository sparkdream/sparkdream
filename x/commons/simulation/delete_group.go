package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func SimulateMsgDeleteGroup(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	cdc codec.Codec,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Pick a random account to act as the Authority (Parent)
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. SETUP: Create native state
		groupName := "sim-delete-target-" + simtypes.RandStringOfLength(r, 5)
		policyAddr := "sim-delete-policy-" + simtypes.RandStringOfLength(r, 10)

		maxSpendPerEpoch := math.NewInt(1000)
		targetGroup := types.Group{
			GroupId:             uint64(simtypes.RandIntBetween(r, 1, 1000)),
			PolicyAddress:       policyAddr,
			ParentPolicyAddress: simAccount.Address.String(), // AUTHORIZATION PASS
			FundingWeight:       0,
			FutarchyEnabled:     false,
			MaxSpendPerEpoch:    &maxSpendPerEpoch,
		}

		if err := k.Groups.Set(ctx, groupName, targetGroup); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "failed to setup group"), nil, err
		}
		if err := k.PolicyToName.Set(ctx, policyAddr, groupName); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "failed to set policy index"), nil, err
		}

		// Add a member so ClearCouncilMembers has something to clear
		if err := k.AddMember(ctx, groupName, types.Member{
			Address: simAccount.Address.String(),
			Weight:  "1",
			AddedAt: ctx.BlockTime().Unix(),
		}); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "failed to add member"), nil, err
		}

		// Set up policy version for veto invalidation
		if err := k.PolicyVersion.Set(ctx, policyAddr, 0); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "failed to set policy version"), nil, err
		}

		// 3. Build the Message
		msg := &types.MsgDeleteGroup{
			Authority: simAccount.Address.String(),
			GroupName: groupName,
		}

		// Ensure we have a ProtoCodec for OperationInput
		protoCdc, ok := cdc.(*codec.ProtoCodec)
		if !ok {
			msgType := sdk.MsgTypeURL(msg)
			return simtypes.NoOpMsg(types.ModuleName, msgType, "codec is not *codec.ProtoCodec"), nil, nil
		}

		// 4. Construct OperationInput
		opInput := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             protoCdc,
			Msg:             msg,
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
			CoinsSpentInMsg: sdk.NewCoins(),
		}

		// 5. Execute
		return simulation.GenAndDeliverTxWithRandFees(opInput)
	}
}
