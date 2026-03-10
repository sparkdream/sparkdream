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

func SimulateMsgVetoGroupProposals(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	cdc codec.Codec,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Pick a random account to act as the Authority (The Parent)
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. SETUP: Create native state for the child group
		groupName := "sim-veto-target-" + simtypes.RandStringOfLength(r, 5)
		policyAddr := "sim-veto-policy-" + simtypes.RandStringOfLength(r, 10)

		maxSpendPerEpoch := math.NewInt(1000)
		targetGroup := types.Group{
			GroupId:             uint64(simtypes.RandIntBetween(r, 1, 1000)),
			PolicyAddress:       policyAddr,
			ParentPolicyAddress: simAccount.Address.String(), // AUTHORIZATION PASS: SimAccount is the Parent
			FundingWeight:       0,
			FutarchyEnabled:     false,
			MaxSpendPerEpoch:    &maxSpendPerEpoch,
		}

		if err := k.Groups.Set(ctx, groupName, targetGroup); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}), "failed to setup group"), nil, err
		}
		if err := k.PolicyToName.Set(ctx, policyAddr, groupName); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}), "failed to set policy index"), nil, err
		}

		// Set up policy version for veto invalidation
		if err := k.PolicyVersion.Set(ctx, policyAddr, 0); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}), "failed to set policy version"), nil, err
		}

		// 3. Build the Message
		msg := &types.MsgVetoGroupProposals{
			Authority: simAccount.Address.String(),
			GroupName: groupName,
		}

		// Ensure we have a ProtoCodec for OperationInput
		protoCdc, ok := cdc.(*codec.ProtoCodec)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "codec is not *codec.ProtoCodec"), nil, nil
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

		// 5. Execute Transaction
		return simulation.GenAndDeliverTxWithRandFees(opInput)
	}
}
