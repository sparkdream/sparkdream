package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCreateChallenge has a member stake DREAM to challenge the work
// submitted on a SUBMITTED or IN_REVIEW initiative it is not assigned to.
// With no such initiative the work of an assigned one is submitted first.
func SimulateMsgCreateChallenge(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCreateChallenge{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		cands, reason, err := e.challengeCandidates()
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, reason)
	})
}

// challengeCandidates returns MsgCreateChallenge candidates on a SUBMITTED or
// IN_REVIEW initiative (submitting one first when there is none), or why
// there are none.
func (e *simEnv) challengeCandidates() ([]candidate, string, error) {
	in, ok, err := e.submittedInitiative(nil)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "no SUBMITTED initiative and none could be submitted", nil
	}
	floor := e.params.MinChallengeStake
	challengers, err := e.members(func(ma memberAcc) bool {
		return unlocked(ma.m).QuoRaw(2).GTE(floor) && !e.k.SameMember(e.ctx, ma.addr(), in.Assignee)
	})
	if err != nil {
		return nil, "", err
	}
	cands := make([]candidate, 0, len(challengers))
	for _, ma := range challengers {
		cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgCreateChallenge{
			Challenger:   ma.addr(),
			InitiativeId: in.Id,
			Reason:       randomReason(e.r),
			Evidence:     []string{randomURI(e.r)},
			StakedDream:  PtrInt(randBetween(e.r, floor, math.MaxInt(floor, unlocked(ma.m).QuoRaw(4)))),
		}, spent: math.ZeroInt()})
	}
	return cands, "no member can stake a challenge", nil
}
