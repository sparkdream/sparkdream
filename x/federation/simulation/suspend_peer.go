package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgSuspendPeer delivers MsgSuspendPeer for an ACTIVE peer from a
// Commons Operations Committee member (a single member may pull this brake)
// and is a NoOp when no sim account holds a seat. Only a policy address can
// resume a peer, so suspension is rare and never takes the last ACTIVE peer.
func SimulateMsgSuspendPeer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgSuspendPeer{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSuspendPeer{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		active, err := e.peersWhere(func(p types.Peer) bool { return p.Status == types.PeerStatus_PEER_STATUS_ACTIVE })
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(active) < 2 {
			return noOp(msg, "keeping the only ACTIVE peer (only a policy can resume it)")
		}
		if e.r.Intn(4) != 0 {
			return noOp(msg, "leaving the peer active")
		}
		p, _ := pick(e.r, active)
		msg.Authority, msg.PeerId, msg.Reason = signer.Address.String(), p.Id, "simulation suspension"
		return e.submit(signer, msg, math.ZeroInt())
	})
}
