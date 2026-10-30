package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgUpdatePeerPolicy delivers MsgUpdatePeerPolicy for a peer that
// is not REMOVED from a Commons Operations Committee member (the handler
// accepts a single member) and is a NoOp when no sim account holds a seat.
// The policy keeps to the handler's validation: known, non-reveal content
// types, reputation flags only on Spark Dream peers, well-formed author
// entries, no content hosts or curation collection.
func SimulateMsgUpdatePeerPolicy(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdatePeerPolicy{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdatePeerPolicy{}
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
		known := federableTypes(e.params.KnownContentTypes)
		sparkDream := p.Type == types.PeerType_PEER_TYPE_SPARK_DREAM
		msg.Authority, msg.PeerId = signer.Address.String(), p.Id
		msg.Policy = types.PeerPolicy{
			PeerId:                       p.Id,
			OutboundContentTypes:         randomContentTypeSubset(e.r, known),
			InboundContentTypes:          randomContentTypeSubset(e.r, known),
			MinOutboundTrustLevel:        commontypes.Saturate[uint32](e.r.Intn(3)),
			InboundRateLimitPerEpoch:     commontypes.Saturate[uint64](e.r.Intn(500) + 10),
			OutboundRateLimitPerEpoch:    commontypes.Saturate[uint64](e.r.Intn(500) + 10),
			AllowReputationQueries:       sparkDream && e.r.Intn(2) == 1,
			AcceptReputationAttestations: sparkDream && e.r.Intn(2) == 1,
			RequireReview:                e.r.Intn(6) == 0,
			AllowedIdentities:            randomAllowedIdentities(e.r, p.Id),
		}
		return e.submit(signer, msg, math.ZeroInt())
	})
}

// federableTypes drops the reveal content types the handler refuses.
func federableTypes(known []string) []string {
	out := make([]string, 0, len(known))
	for _, ct := range known {
		if ct != "reveal_proposal" && ct != "reveal_tranche" {
			out = append(out, ct)
		}
	}
	return out
}

// randomAllowedIdentities picks an author gate for a peer: mostly open, some
// of the time just one "@name@<peer>" handle the submit op can use, rarely
// nobody (the default for a new peer).
func randomAllowedIdentities(r *rand.Rand, peerID string) []string {
	switch r.Intn(6) {
	case 0:
		return nil
	case 1:
		return []string{fmt.Sprintf("@%s@%s", randomCreatorName(r), peerID)}
	default:
		return []string{types.AllIdentities}
	}
}
