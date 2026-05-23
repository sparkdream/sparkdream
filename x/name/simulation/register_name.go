package simulation

import (
	"math/rand"
	"strings"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgRegisterName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	rk types.RepKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Get Current Params
		params := k.GetParams(ctx)

		// 2. Pick a sim account that is an active x/rep member. If none exist,
		// skip — name registration requires a real Member record and
		// simulation cannot conjure one without crossing module boundaries.
		var simAccount simtypes.Account
		var found bool
		if rk != nil {
			perm := r.Perm(len(accs))
			for _, idx := range perm {
				acc := accs[idx]
				if rk.IsActiveMember(ctx, acc.Address) {
					simAccount = acc
					found = true
					break
				}
			}
		}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "no active x/rep member among sim accounts"), nil, nil
		}

		// 4. Check Solvency (registration fee + explicit gas fees of 5M uspark)
		explicitFees := math.NewInt(5000000)
		totalRequired := explicitFees
		if !params.RegistrationFeeAmount.IsZero() {
			totalRequired = totalRequired.Add(params.RegistrationFeeAmount)
		}
		balance := bk.SpendableCoins(ctx, simAccount.Address)
		if balance.AmountOf(sdk.DefaultBondDenom).LT(totalRequired) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "insufficient funds for reg fee + gas"), nil, nil
		}

		// 4.5. CHECK NAME LIMIT
		const MaxNames = 5

		count, err := k.GetOwnedNamesCount(ctx, simAccount.Address)
		if err == nil {
			if count >= MaxNames {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "max names reached for account"), nil, nil
			}
		}

		// 5. Generate Valid Name
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

		for _, blocked := range params.BlockedNames {
			if name == blocked {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "generated blocked name"), nil, nil
			}
		}

		// Check collision to be safe
		_, foundName := k.GetName(ctx, name)
		if foundName {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "name already exists"), nil, nil
		}

		// 6. Construct Message
		msg := &types.MsgRegisterName{
			Authority: simAccount.Address.String(),
			Name:      name,
			Data:      simtypes.RandStringOfLength(r, 20),
		}

		// 7. Execute Transaction
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, params.RegistrationFeeAmount)),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		// Define explicit high fees to satisfy the AnteHandler check (5M uspark)
		fees := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(5000000)))

		return simulation.GenAndDeliverTx(opMsg, fees)
	}
}
