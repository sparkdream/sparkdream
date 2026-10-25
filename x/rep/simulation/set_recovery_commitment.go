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

func SimulateMsgSetRecoveryCommitment(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		member, simAccount, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetRecoveryCommitment{}), "failed to get/create member"), nil, nil
		}
		if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE || k.IsTombstoned(ctx, member.Address) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetRecoveryCommitment{}), "member not active"), nil, nil
		}
		if set, ok := k.GetRecoveryCommitments(ctx, member.Address); ok && len(set.Commitments) >= types.MaxRecoveryCommitments {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetRecoveryCommitment{}), "commitment cap reached"), nil, nil
		}
		preimage := make([]byte, 16)
		r.Read(preimage)
		msg := &types.MsgSetRecoveryCommitment{Member: member.Address, Commitment: types.RecoveryCommitmentHash(preimage)}
		return deliver(r, app, ctx, msg, simAccount, ak, bk, txGen)
	}
}
