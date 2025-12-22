package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgForceUpgrade simulates the MsgForceUpgrade message.
func SimulateMsgForceUpgrade(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	cdc codec.Codec,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 1. Construct the Upgrade Plan
		plan := types.UpgradePlan{
			Name:   "simulation-upgrade-" + simtypes.RandStringOfLength(r, 5),
			Height: ctx.BlockHeight() + int64(simtypes.RandIntBetween(r, 100, 1000)),
			Info:   "simulated upgrade",
		}

		msg := &types.MsgForceUpgrade{
			Authority: simAccount.Address.String(),
			Plan:      plan,
		}

		// 2. Setup Permissions
		// The MsgForceUpgrade handler strictly enforces RBAC.
		// For the simulation to succeed (happy path), we must grant the 'MsgForceUpgrade' permission to the signer.
		msgType := sdk.MsgTypeURL(msg)
		err := k.SetPolicyPermissions(ctx, simAccount.Address.String(), types.PolicyPermissions{
			PolicyAddress:   simAccount.Address.String(),
			AllowedMessages: []string{msgType},
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to set permissions"), nil, err
		}

		// 3. Generate Fees
		spendable := bk.SpendableCoins(ctx, simAccount.Address)
		coins, err := randomFees(r, ctx, spendable)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to generate fees"), nil, err
		}

		// Ensure we have a ProtoCodec for OperationInput
		protoCdc, ok := cdc.(*codec.ProtoCodec)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "codec is not *codec.ProtoCodec"), nil, nil
		}

		// 4. Deliver Transaction
		// We use the OperationInput struct defined in x/simulation/util.go
		opMsg, futureOps, err := simulation.GenAndDeliverTx(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             protoCdc,
			Msg:             msg,
			CoinsSpentInMsg: sdk.Coins{}, // Required field
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk, // NOTE: SDK uses 'Bankkeeper' (lowercase k)
			ModuleName:      types.ModuleName,
		}, coins)

		return opMsg, futureOps, err
	}
}

// randomFees generates random fees from the spendable coins.
// This is a local helper to avoid "undefined: simulation.RandomFees" errors if using older SDK imports.
func randomFees(r *rand.Rand, ctx sdk.Context, spendableCoins sdk.Coins) (sdk.Coins, error) {
	if spendableCoins.Empty() {
		return nil, nil
	}

	perm := r.Perm(len(spendableCoins))
	var randCoins sdk.Coins

	for _, i := range perm {
		coin := spendableCoins[i]
		if !coin.Amount.IsPositive() {
			continue
		}

		// Select a random amount up to the balance
		amt, err := simtypes.RandPositiveInt(r, coin.Amount)
		if err != nil {
			return nil, err
		}

		randCoins = randCoins.Add(sdk.NewCoin(coin.Denom, amt))
	}

	return randCoins, nil
}
