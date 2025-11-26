package simulation

import (
	"math/rand"
	"strings"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgRegisterName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	gk groupkeeper.Keeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Select a random simulation account
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. Satisfy "Council Member" constraint
		// The MsgServer requires the signer to be a member of params.CouncilGroupId.
		// We create a new group with our account as a member and update the params to point to it.
		// This creates a valid "sandbox" state for the transaction to succeed.
		members := []group.MemberRequest{
			{
				Address:  simAccount.Address.String(),
				Weight:   "1",
				Metadata: "sim member",
			},
		}

		createGroupMsg := &group.MsgCreateGroup{
			Admin:    simAccount.Address.String(),
			Members:  members,
			Metadata: "sim council for registration",
		}

		// Create the group immediately (bypassing gas/fees for setup)
		groupRes, err := gk.CreateGroup(ctx, createGroupMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to create sim group"), nil, err
		}

		// Update x/name params to use this new group
		params := k.GetParams(ctx)
		params.CouncilGroupId = groupRes.GroupId
		if err := k.SetParams(ctx, params); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to update params"), nil, err
		}

		// 3. Check Solvency (Registration Fee)
		if !params.RegistrationFee.IsZero() {
			balance := bk.SpendableCoins(ctx, simAccount.Address)
			if balance.AmountOf(params.RegistrationFee.Denom).LT(params.RegistrationFee.Amount) {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "insufficient funds for reg fee"), nil, nil
			}
		}

		// 4. Generate Valid Name
		minLen := int(params.MinNameLength)
		if minLen <= 0 {
			minLen = 3
		}
		maxLen := int(params.MaxNameLength)
		if maxLen <= minLen {
			maxLen = minLen + 10
		}

		nameLen := minLen + r.Intn(maxLen-minLen+1)
		name := strings.ToLower(simtypes.RandStringOfLength(r, nameLen))

		// Avoid Blocked Names
		for _, blocked := range params.BlockedNames {
			if name == blocked {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "generated blocked name"), nil, nil
			}
		}

		// 5. Construct Message
		msg := &types.MsgRegisterName{
			Authority: simAccount.Address.String(),
			Name:      name,
			Data:      simtypes.RandStringOfLength(r, 20),
		}

		// 6. Execute Transaction
		// We expect this to succeed now that the user is a council member.
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(params.RegistrationFee),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
