package simulation

import (
	"math/rand"
	"strings"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgSetPrimary(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Select a random simulation account
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. Fetch Params for Valid Name Generation
		params := k.GetParams(ctx)
		minLen := int(params.MinNameLength)
		if minLen <= 0 {
			minLen = 3
		}
		maxLen := int(params.MaxNameLength)
		if maxLen <= minLen {
			maxLen = minLen + 10
		}

		// Generate random length between Min and Max
		nameLen := minLen + r.Intn(maxLen-minLen+1)
		name := strings.ToLower(simtypes.RandStringOfLength(r, nameLen))
		data := simtypes.RandStringOfLength(r, 25)

		// 3. Validation Checks
		// A. Check Blocked Names
		for _, blocked := range params.BlockedNames {
			if name == blocked {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetPrimary{}), "generated blocked name"), nil, nil
			}
		}

		// B. Check Collision (Don't overwrite existing state)
		_, found := k.GetName(ctx, name)
		if found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetPrimary{}), "name already exists"), nil, nil
		}

		// 4. Setup Pre-conditions: Inject State
		// We manually register the name to avoid paying fees or passing Council checks in this specific op.
		record := types.NameRecord{
			Name:  name,
			Owner: simAccount.Address.String(),
			Data:  data,
		}

		// Save the main record
		err := k.SetName(ctx, record)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetPrimary{}), "failed to set name record"), nil, err
		}

		// Update the secondary index (Owner -> Names)
		err = k.AddNameToOwner(ctx, simAccount.Address, name)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetPrimary{}), "failed to update owner index"), nil, err
		}

		// 5. Construct the MsgSetPrimary
		msg := &types.MsgSetPrimary{
			Authority: simAccount.Address.String(),
			Name:      name,
		}

		// 6. Execute Transaction
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(), // No fees for SetPrimary usually
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
