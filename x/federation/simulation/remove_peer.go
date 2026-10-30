package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgRemovePeer is always a NoOp. Removal starts the irreversible
// cleanup of a peer's content, links and attestations, so RemovePeer accepts
// only x/gov or the Commons Council / Operations Committee policy address
// (isCouncilPolicy), never a single member. Sim accounts are never made a
// group or committee policy.
func SimulateMsgRemovePeer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRemovePeer{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgRemovePeer{}, "policy-only: needs x/gov or a commons council/committee policy address")
	})
}
