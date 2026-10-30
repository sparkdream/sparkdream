package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgUpdateOperationalParams is always a NoOp: the handler accepts
// only x/gov or the Commons Council / Operations Committee policy address
// (isCouncilPolicy), never a single member. Sim accounts are never made a
// group or committee policy.
func SimulateMsgUpdateOperationalParams(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdateOperationalParams{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgUpdateOperationalParams{}, "policy-only: needs x/gov or a commons council/committee policy address")
	})
}
