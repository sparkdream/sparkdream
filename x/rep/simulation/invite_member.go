package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// requiredInvitationStakeForSim mirrors keeper.computeRequiredInvitationStake
// (unexported): the floor escalates with the credits used this season.
func requiredInvitationStakeForSim(params types.Params, member types.Member) math.Int {
	maxCredits := keeper.GetMaxInvitationCredits(params.TrustLevelConfig, member.TrustLevel)
	if maxCredits <= member.InvitationCredits {
		return params.MinInvitationStake
	}
	used := uint64(maxCredits - member.InvitationCredits)
	if used == 0 || params.InvitationCostMultiplier.LTE(math.LegacyOneDec()) {
		return params.MinInvitationStake
	}
	return params.InvitationCostMultiplier.Power(used).MulInt(params.MinInvitationStake).TruncateInt()
}

// inviteCandidates returns MsgInviteMember candidates: members with a credit
// and the DREAM for the required stake, each paired with a random live
// non-member that has no pending invitation.
func (e *simEnv) inviteCandidates() ([]candidate, error) {
	var invitees []simtypes.Account
	for _, acc := range e.nonMembers() {
		pending, err := e.pendingInvitationTo(acc.Address.String())
		if err != nil {
			return nil, err
		}
		if !pending {
			invitees = append(invitees, acc)
		}
	}
	if len(invitees) == 0 {
		return nil, nil
	}
	inviters, err := e.members(func(ma memberAcc) bool {
		return ma.m.InvitationCredits > 0 &&
			unlocked(ma.m).QuoRaw(2).GTE(requiredInvitationStakeForSim(e.params, ma.m))
	})
	if err != nil {
		return nil, err
	}
	out := make([]candidate, 0, len(inviters))
	for _, ma := range inviters {
		invitee := invitees[e.r.Intn(len(invitees))]
		floor := requiredInvitationStakeForSim(e.params, ma.m)
		stake := randBetween(e.r, floor, math.MaxInt(floor, unlocked(ma.m).QuoRaw(4)))
		tags, err := e.tagsFor(ma.m)
		if err != nil {
			return nil, err
		}
		out = append(out, candidate{acc: ma.acc, msg: &types.MsgInviteMember{
			Inviter:        ma.addr(),
			InviteeAddress: invitee.Address.String(),
			StakedDream:    PtrInt(stake),
			VouchedTags:    tags,
		}, spent: math.ZeroInt()})
	}
	return out, nil
}

// SimulateMsgInviteMember has a member with an invitation credit stake DREAM
// on a live non-member sim account.
func SimulateMsgInviteMember(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgInviteMember{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		cands, err := e.inviteCandidates()
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 6, "no member can invite a live non-member")
	})
}
