package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgBondVerifier(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Check if already a verifier
		_, err := k.Verifiers.Get(ctx, addr)
		if err == nil {
			// Already bonded, add more bond
			v, _ := k.Verifiers.Get(ctx, addr)
			addAmount := math.NewInt(int64(r.Intn(500) + 100))
			v.CurrentBond = v.CurrentBond.Add(addAmount)
			if err := k.Verifiers.Set(ctx, addr, v); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgBondVerifier{}), "failed to update verifier"), nil, nil
			}
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgBondVerifier{}), "ok (direct keeper call)"), nil, nil
		}

		bondAmount := math.NewInt(int64(r.Intn(1000) + 500))
		verifier := types.FederationVerifier{
			Address:            addr,
			CurrentBond:        bondAmount,
			TotalCommittedBond: math.ZeroInt(),
			BondStatus:         types.VerifierBondStatus_VERIFIER_BOND_STATUS_NORMAL,
			BondedAt:           ctx.BlockTime().Unix(),
		}

		if err := k.Verifiers.Set(ctx, addr, verifier); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgBondVerifier{}), "failed to set verifier"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgBondVerifier{}), "ok (direct keeper call)"), nil, nil
	}
}
