package simulation

import (
	"math/rand"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func SimulateMsgVetoGroupProposals(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	gk groupkeeper.Keeper, // Required to create real x/group state
	k keeper.Keeper,
	cdc codec.Codec, // Required for OperationInput
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Pick a random account to act as the Authority (The Parent)
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. SETUP: Create Real x/group State for the Child
		// The Handler requires the Admin of the Child Policy to be the x/commons Module Account.
		// This is because the handler uses the module account's authority to bump the policy version.
		moduleAddr := ak.GetModuleAddress(types.ModuleName)
		if moduleAddr == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}), "module account not found"), nil, nil
		}

		// A. Create Child Group (Admin = Module)
		groupRes, err := gk.CreateGroup(ctx, &group.MsgCreateGroup{
			Admin:    moduleAddr.String(),
			Members:  []group.MemberRequest{{Address: simAccount.Address.String(), Weight: "1"}},
			Metadata: "Simulation Target Child Group",
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}), "failed to create x/group group"), nil, err
		}

		// B. Create Child Policy (Admin = Module)
		decisionPolicy := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
		policyAny, err := codectypes.NewAnyWithValue(decisionPolicy)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}), "failed to pack policy"), nil, err
		}

		policyRes, err := gk.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
			Admin:          moduleAddr.String(),
			GroupId:        groupRes.GroupId,
			DecisionPolicy: policyAny,
			Metadata:       "Simulation Target Policy",
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}), "failed to create x/group policy"), nil, err
		}

		// 3. Inject into x/commons Keeper State
		// We set ParentPolicyAddress to the simAccount so it is authorized to sign the Veto message.
		groupName := "sim_veto_target_" + simtypes.RandStringOfLength(r, 5)

		targetGroup := types.ExtendedGroup{
			GroupId:             groupRes.GroupId,
			PolicyAddress:       policyRes.Address,
			ParentPolicyAddress: simAccount.Address.String(), // AUTHORIZATION PASS: SimAccount is the Parent
			FundingWeight:       0,
			FutarchyEnabled:     false,
			MaxSpendPerEpoch:    "1000uspark",
		}

		if err := k.ExtendedGroup.Set(ctx, groupName, targetGroup); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}), "failed to setup extended group"), nil, err
		}

		// 4. Build the Message
		msg := &types.MsgVetoGroupProposals{
			Authority: simAccount.Address.String(),
			GroupName: groupName,
		}

		// Ensure we have a ProtoCodec for OperationInput
		protoCdc, ok := cdc.(*codec.ProtoCodec)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "codec is not *codec.ProtoCodec"), nil, nil
		}

		// 5. Construct OperationInput
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

		// 6. Execute Transaction
		return simulation.GenAndDeliverTxWithRandFees(opInput)
	}
}
