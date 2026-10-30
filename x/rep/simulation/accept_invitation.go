package simulation

import (
	"crypto/sha256"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// recoveryPreimage is the simulation's recovery secret for addr: derived
// from the address, so the recovery ops can rebuild it without state.
func recoveryPreimage(addr string) []byte {
	sum := sha256.Sum256([]byte("sparkdream/sim/recovery/" + addr))
	return sum[:]
}

// SimulateMsgAcceptInvitation has the invitee of a PENDING invitation accept
// it (agreeing to the chain's content license), usually registering
// an onboarding recovery commitment the recovery ops can later reveal. With
// no pending invitation to a live sim account, a member invites one first.
func SimulateMsgAcceptInvitation(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAcceptInvitation{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		pending := func() ([]types.Invitation, error) {
			return e.invitations(func(inv types.Invitation) bool {
				_, ok := e.account(inv.InviteeAddress)
				return ok && inv.Status == types.InvitationStatus_INVITATION_STATUS_PENDING
			})
		}
		invs, err := pending()
		if err != nil {
			return noOpErr(label, err)
		}
		if len(invs) == 0 {
			cands, err := e.inviteCandidates()
			if err != nil {
				return noOpErr(label, err)
			}
			opMsg, fops, err := e.firstValid(&types.MsgInviteMember{}, cands, 4, "no member can invite")
			if err != nil || !opMsg.OK {
				return opMsg, fops, err
			}
			if invs, err = pending(); err != nil {
				return noOpErr(label, err)
			}
		}
		cands := make([]candidate, 0, len(invs))
		for _, inv := range invs {
			acc, _ := e.account(inv.InviteeAddress)
			msg := &types.MsgAcceptInvitation{
				Invitee:                inv.InviteeAddress,
				InvitationId:           inv.Id,
				AcceptedContentLicense: commontypes.ChainContentLicense,
			}
			if e.r.Intn(4) != 0 {
				msg.RecoveryCommitment = types.RecoveryCommitmentHash(recoveryPreimage(inv.InviteeAddress))
			}
			cands = append(cands, candidate{acc: acc, msg: msg, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 6, "no pending invitation to a live sim account")
	})
}
