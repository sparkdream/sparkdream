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

func SimulateMsgDeleteGroup(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	gk groupkeeper.Keeper,
	k keeper.Keeper,
	cdc codec.Codec,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Pick a random account to act as the Authority (Parent)
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. SETUP: Create Real x/group State
		// The Handler requires the Admin to be the Module Account (for the "Zombie Kill" step).
		moduleAddr := ak.GetModuleAddress(types.ModuleName)
		if moduleAddr == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "module account not found"), nil, nil
		}

		// A. Create Group (Admin = Module)
		groupRes, err := gk.CreateGroup(ctx, &group.MsgCreateGroup{
			Admin:    moduleAddr.String(),
			Members:  []group.MemberRequest{{Address: simAccount.Address.String(), Weight: "1"}},
			Metadata: "Simulated Group",
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "failed to create x/group group"), nil, err
		}

		// B. Create Policy (Admin = Module)
		decisionPolicy := group.NewThresholdDecisionPolicy("1", time.Hour, 0)
		policyAny, err := codectypes.NewAnyWithValue(decisionPolicy)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "failed to pack policy"), nil, err
		}

		policyRes, err := gk.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
			Admin:          moduleAddr.String(),
			GroupId:        groupRes.GroupId,
			DecisionPolicy: policyAny,
			Metadata:       "Simulated Policy",
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "failed to create x/group policy"), nil, err
		}

		// 3. Inject into x/commons Keeper State
		groupName := "sim_delete_target_" + simtypes.RandStringOfLength(r, 5)

		targetGroup := types.ExtendedGroup{
			GroupId:             groupRes.GroupId,            // Real ID
			PolicyAddress:       policyRes.Address,           // Real Policy Address
			ParentPolicyAddress: simAccount.Address.String(), // AUTHORIZATION PASS
			FundingWeight:       0,
			FutarchyEnabled:     false,
			MaxSpendPerEpoch:    "1000uspark",
		}

		if err := k.ExtendedGroup.Set(ctx, groupName, targetGroup); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteGroup{}), "failed to setup extended group"), nil, err
		}

		// 4. Build the Message
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

		// 6. Execute
		return simulation.GenAndDeliverTxWithRandFees(opInput)
	}
}
