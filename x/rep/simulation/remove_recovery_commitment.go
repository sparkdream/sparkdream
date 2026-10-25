package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgRemoveRecoveryCommitment(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		member, simAccount, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRemoveRecoveryCommitment{}), "failed to get/create member"), nil, nil
		}
		if k.IsTombstoned(ctx, member.Address) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRemoveRecoveryCommitment{}), "member tombstoned"), nil, nil
		}
		set, ok := k.GetRecoveryCommitments(ctx, member.Address)
		if !ok || len(set.Commitments) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRemoveRecoveryCommitment{}), "member has no recovery commitment"), nil, nil
		}
		msg := &types.MsgRemoveRecoveryCommitment{Member: member.Address, Commitment: set.Commitments[r.Intn(len(set.Commitments))].Commitment}
		return deliver(r, app, ctx, msg, simAccount, ak, bk, txGen)
	}
}
