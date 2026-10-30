package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// verifyTarget is a REVEALED tranche and a staker on it who may still vote.
type verifyTarget struct {
	ref   trancheRef
	voter simtypes.Account
}

// verifyTargets returns the REVEALED tranches with a live staker (not the
// contributor) who has not voted yet.
func (e *simEnv) verifyTargets() ([]verifyTarget, error) {
	var out []verifyTarget
	refs, err := e.tranchesIn(types.TrancheStatus_TRANCHE_STATUS_REVEALED, func(trancheRef) bool { return true })
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		stakes, err := e.trancheStakes(ref.c.Id, ref.t.Id)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, s := range stakes {
			if seen[s.Staker] {
				continue
			}
			seen[s.Staker] = true
			acc, ok := e.account(s.Staker)
			if !ok || e.sameMember(s.Staker, ref.c.Contributor) || e.hasVoted(ref.c.Id, ref.t.Id, s.Staker) {
				continue
			}
			out = append(out, verifyTarget{ref: ref, voter: acc})
		}
	}
	return out, nil
}

// SimulateMsgVerify has a staker of a REVEALED tranche vote on it (two in three
// confirming the value, with a 1-5 quality rating). When no vote is open it
// first reveals a BACKED tranche (backing one with real stakes when needed).
func SimulateMsgVerify(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgVerify{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		targets, err := e.verifyTargets()
		if err != nil {
			return noOpErr(&types.MsgVerify{}, err)
		}
		if len(targets) == 0 {
			if _, _, err := e.ensureRevealed(); err != nil {
				return noOpErr(&types.MsgVerify{}, err)
			}
			if targets, err = e.verifyTargets(); err != nil {
				return noOpErr(&types.MsgVerify{}, err)
			}
		}
		tg, ok := pick(e.r, targets)
		if !ok {
			return noOp(&types.MsgVerify{}, "no REVEALED tranche with a staker left to vote")
		}
		msg := &types.MsgVerify{
			Voter:          tg.voter.Address.String(),
			ContributionId: tg.ref.c.Id,
			TrancheId:      tg.ref.t.Id,
			ValueConfirmed: e.r.Intn(3) != 0,
			QualityRating:  commontypes.Saturate[uint32](1 + e.r.Intn(5)),
			Comments:       "Reviewed the revealed tranche against its preview",
		}
		return e.submit(tg.voter, msg)
	})
}
