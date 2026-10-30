package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgRevokeInvitation has an inviter withdraw one of its PENDING
// invitations (stake unlocked, credit restored).
func SimulateMsgRevokeInvitation(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRevokeInvitation{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		invs, err := e.invitations(func(inv types.Invitation) bool {
			_, ok := e.account(inv.Inviter)
			return ok && inv.Status == types.InvitationStatus_INVITATION_STATUS_PENDING
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if len(invs) == 0 {
			// Prerequisite: a member invites someone.
			cands, err := e.inviteCandidates()
			if err != nil {
				return noOpErr(label, err)
			}
			for n, i := range e.r.Perm(len(cands)) {
				if n >= 4 {
					break
				}
				inv, isInvite := cands[i].msg.(*types.MsgInviteMember)
				if !isInvite {
					continue
				}
				ok, err := e.ensure(cands[i].acc, inv, math.ZeroInt())
				if err != nil {
					return noOpErr(label, err)
				}
				if ok {
					invs, err = e.invitations(func(x types.Invitation) bool {
						return x.InviteeAddress == inv.InviteeAddress && x.Status == types.InvitationStatus_INVITATION_STATUS_PENDING
					})
					if err != nil {
						return noOpErr(label, err)
					}
					break
				}
			}
		}
		cands := make([]candidate, 0, len(invs))
		for _, inv := range invs {
			acc, _ := e.account(inv.Inviter)
			cands = append(cands, candidate{acc: acc, msg: &types.MsgRevokeInvitation{
				Inviter:      inv.Inviter,
				InvitationId: inv.Id,
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no pending invitation from a live sim account")
	})
}
