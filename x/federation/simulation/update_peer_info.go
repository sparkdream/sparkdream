package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgUpdatePeerInfo delivers MsgUpdatePeerInfo for a peer that is not
// REMOVED from a Commons Operations Committee member (the handler accepts a
// single member) and is a NoOp when no sim account holds a seat. The handler
// does not validate the descriptive fields, so any display name / metadata
// is accepted; metadata is sometimes cleared to exercise the replace rule.
func SimulateMsgUpdatePeerInfo(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdatePeerInfo{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdatePeerInfo{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		peers, err := e.peersWhere(func(p types.Peer) bool { return p.Status != types.PeerStatus_PEER_STATUS_REMOVED })
		if err != nil {
			return noOpErr(msg, err)
		}
		p, ok := pick(e.r, peers)
		if !ok {
			return noOp(msg, "no registered peer")
		}
		msg.Authority, msg.PeerId = signer.Address.String(), p.Id
		msg.DisplayName = simtypes.RandStringOfLength(e.r, 12)
		if e.r.Intn(4) != 0 {
			msg.Metadata = fmt.Sprintf(`{"note":%q}`, simtypes.RandStringOfLength(e.r, 16))
		}
		return e.submit(signer, msg, math.ZeroInt())
	})
}
