package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgTransferDream sends DREAM between members: a tip (capped at
// max_tip_amount) to any member, or a gift to one of the sender's invitees.
func SimulateMsgTransferDream(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgTransferDream{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ms, err := e.members(func(ma memberAcc) bool { return unlocked(ma.m).GTE(dream(2)) })
		if err != nil {
			return noOpErr(label, err)
		}
		all, err := e.members(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, from := range ms {
			// Gift to an invitee when the sender has one.
			var invitees []memberAcc
			for _, to := range all {
				if to.m.InvitedBy == from.addr() {
					invitees = append(invitees, to)
				}
			}
			purpose := types.TransferPurpose_TRANSFER_PURPOSE_TIP
			to, ok := pick(e.r, invitees)
			if ok && e.r.Intn(2) == 0 {
				purpose = types.TransferPurpose_TRANSFER_PURPOSE_GIFT
			} else if to, ok = pick(e.r, all); !ok || to.addr() == from.addr() {
				continue
			}
			hi := math.MinInt(unlocked(from.m).QuoRaw(4), dream(50))
			if purpose == types.TransferPurpose_TRANSFER_PURPOSE_TIP {
				hi = math.MinInt(hi, e.params.MaxTipAmount)
			}
			amt := randBetween(e.r, math.MinInt(dream(1), hi), hi)
			if !amt.IsPositive() {
				continue
			}
			cands = append(cands, candidate{acc: from.acc, msg: &types.MsgTransferDream{
				Sender:    from.addr(),
				Recipient: to.addr(),
				Amount:    PtrInt(amt),
				Purpose:   purpose,
				Reference: "sim",
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 6, "no member can send DREAM")
	})
}
