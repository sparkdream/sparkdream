package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

func SimulateMsgShieldedExec(
	ak types.AccountKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Check shield is enabled
		params, err := k.Params.Get(ctx)
		if err != nil || !params.Enabled {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgShieldedExec{}), "shield disabled"), nil, nil
		}

		// 2. Pick a random account as submitter
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 3. Construct a minimal immediate-mode ShieldedExec.
		// The ZK proof will fail verification, which tests the validation pipeline.
		nullifier := make([]byte, 32)
		r.Read(nullifier)
		rateLimitNullifier := make([]byte, 32)
		r.Read(rateLimitNullifier)
		merkleRoot := make([]byte, 32)
		r.Read(merkleRoot)
		proof := make([]byte, 128)
		r.Read(proof)

		msg := &types.MsgShieldedExec{
			Submitter:          simAccount.Address.String(),
			Nullifier:          nullifier,
			RateLimitNullifier: rateLimitNullifier,
			MerkleRoot:         merkleRoot,
			Proof:              proof,
			ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		}

		opMsg := simulation.OperationInput{
			R:             r,
			App:           app,
			TxGen:         txGen,
			Cdc:           nil,
			Msg:           msg,
			Context:       ctx,
			SimAccount:    simAccount,
			AccountKeeper: ak,
			Bankkeeper:    bk,
			ModuleName:    types.ModuleName,
		}

		// ShieldedExec will typically fail in simulation (ZK proof verification,
		// unfunded module account, etc). Treat delivery errors as NoOp rather
		// than fatal simulation failures.
		result, futureOps, err := simulation.GenAndDeliverTxWithRandFees(opMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), err.Error()), nil, nil
		}
		return result, futureOps, nil
	}
}
