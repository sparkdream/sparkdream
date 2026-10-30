package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgRegisterPeer delivers MsgRegisterPeer from a sim account that
// is a Commons Operations Committee member (the only non-policy signer the
// handler accepts) and is a NoOp when none is. The peer starts PENDING;
// only a policy address can activate it (MsgResumePeer).
func SimulateMsgRegisterPeer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRegisterPeer{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRegisterPeer{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		peerID := randomPeerID(e.r)
		if existing, found := e.peer(peerID); found && existing.Status != types.PeerStatus_PEER_STATUS_REMOVED {
			return noOp(msg, "peer id already registered")
		}
		msg.Authority = signer.Address.String()
		msg.PeerId = peerID
		msg.DisplayName = "Sim Peer " + peerID
		msg.Type = randomPeerType(e.r)
		msg.Metadata = fmt.Sprintf("simulation peer registered at height %d", e.ctx.BlockHeight())
		return e.submit(signer, msg, math.ZeroInt())
	})
}
