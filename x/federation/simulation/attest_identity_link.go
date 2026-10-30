package simulation

import (
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgAttestIdentityLink delivers MsgAttestIdentityLink: a working
// bridge operator of the link's peer (registering one first when none can
// attest) vouches for an UNVERIFIED, unexpired link of another account
// (linking one first through MsgLinkIdentity when none exists),
// echoing the link's challenge and citing evidence on the peer's own host.
// A NoOp when no such link exists (links need an ACTIVE bridged peer) or
// its remote identity is already verified to someone else.
func SimulateMsgAttestIdentityLink(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAttestIdentityLink{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAttestIdentityLink{}
		l, ok, err := e.ensureLink(func(l types.IdentityLink) bool {
			if l.Status != types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED || len(l.Challenge) == 0 {
				return false
			}
			if l.UnverifiedExpiresAt != 0 && e.now() >= l.UnverifiedExpiresAt {
				return false
			}
			p, ok := e.peer(l.PeerId)
			if !ok || !activeBridged(p) {
				return false
			}
			owner, err := e.k.IdentityLinksByRemote.Get(e.ctx, collections.Join(l.PeerId, l.RemoteIdentity))
			return err != nil || owner == l.LocalAddress
		}, anyone)
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no unverified, unexpired identity link on an ACTIVE bridged peer")
		}
		if pol, ok := e.policy(l.PeerId); ok {
			if p, _ := e.peer(l.PeerId); p.Type == types.PeerType_PEER_TYPE_ACTIVITYPUB &&
				types.CreatorIdentityHostAllowed(l.PeerId, pol.ContentHosts, l.RemoteIdentity) != nil {
				return noOp(msg, "remote identity is not on the peer's host")
			}
		}
		b, ok, err := e.ensureBinding(
			func(b types.BridgeBinding) bool {
				return b.PeerId == l.PeerId && !e.sameMember(b.Address, l.LocalAddress)
			},
			func(p types.Peer) bool { return p.Id == l.PeerId },
			func(a simtypes.Account) bool { return !e.sameMember(a.Address.String(), l.LocalAddress) },
		)
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no bridge operator of the peer other than the link holder")
		}
		acc, ok := e.account(b.Address)
		if !ok {
			return noOp(msg, "operator is not a live sim account")
		}
		msg.Operator, msg.PeerId, msg.LocalAddress = b.Address, l.PeerId, l.LocalAddress
		msg.RemoteIdentity = l.RemoteIdentity
		msg.Challenge = hex.EncodeToString(l.Challenge)
		msg.EvidenceUri = fmt.Sprintf("https://%s/users/sim/statuses/%d", l.PeerId, e.r.Intn(1_000_000_000))
		return e.submit(acc, msg, math.ZeroInt())
	})
}
