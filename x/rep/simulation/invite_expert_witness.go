package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgInviteExpertWitness has a juror seated on a pending initiative
// jury review call an ESTABLISHED+ sim member as expert witness. The handler
// decides tag reputation and conflicts; firstValid dry-runs each pairing.
func SimulateMsgInviteExpertWitness(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgInviteExpertWitness{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		height := e.ctx.BlockHeight()
		rvs, err := e.juryReviews(func(rv types.JuryReview) bool {
			return rv.Verdict == types.Verdict_VERDICT_PENDING && rv.InitiativeId != 0 && rv.ChallengeId != 0 &&
				rv.ContentChallengeId == 0 && height < rv.Deadline &&
				len(rv.ExpertWitnesses) < keeper.MaxExpertWitnessesPerReview
		})
		if err != nil {
			return noOpErr(label, err)
		}
		var experts []simtypes.Account
		for _, acc := range e.accs {
			m, err := k.Member.Get(e.ctx, acc.Address.String())
			if err == nil && m.Status == types.MemberStatus_MEMBER_STATUS_ACTIVE &&
				m.TrustLevel >= types.TrustLevel_TRUST_LEVEL_ESTABLISHED {
				experts = append(experts, acc)
			}
		}
		if len(experts) == 0 {
			return noOp(label, "no ESTABLISHED+ sim member to call as expert")
		}
		var cands []candidate
		for _, rv := range rvs {
			for _, j := range rv.Jurors {
				acc, ok := e.account(j)
				if !ok {
					continue
				}
				expert := experts[e.r.Intn(len(experts))]
				cands = append(cands, candidate{acc: acc, msg: &types.MsgInviteExpertWitness{
					Juror: j, JuryReviewId: rv.Id, Expert: expert.Address.String(),
				}, spent: math.ZeroInt()})
			}
		}
		return e.firstValid(label, cands, 6, "no seated sim juror on a pending initiative review with an eligible expert")
	})
}
