package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgResumePeer is always a NoOp. Activating or resuming a peer is
// the federation's trust decision: ResumePeer accepts only x/gov or the
// Commons Council / Operations Committee policy address (an executed
// proposal, IsCouncilOrCommitteePolicy), never a single member's signature.
// Sim accounts are never made a group or committee policy.
func SimulateMsgResumePeer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgResumePeer{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgResumePeer{}, "policy-only: needs x/gov or a commons council/committee policy address")
	})
}
