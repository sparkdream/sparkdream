package simulation

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgChallengeContent has a member stake DREAM to challenge content
// backed by an author bond (blog, forum, collect) it did not write. Author
// bonds come from the content modules' own simulations.
func SimulateMsgChallengeContent(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgChallengeContent{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bonds, err := e.stakes(func(s types.Stake) bool {
			if !types.IsAuthorBondType(s.TargetType) {
				return false
			}
			has, err := e.k.ContentChallengesByTarget.Has(e.ctx, collections.Join(int32(s.TargetType), s.TargetId))
			return err == nil && !has
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if len(bonds) == 0 {
			return noOp(label, "no unchallenged author-bonded content")
		}
		floor := e.params.MinChallengeStake
		challengers, err := e.members(func(ma memberAcc) bool { return unlocked(ma.m).QuoRaw(2).GTE(floor) })
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, ma := range challengers {
			b := bonds[e.r.Intn(len(bonds))]
			if e.k.SameMember(e.ctx, ma.addr(), b.Staker) {
				continue
			}
			cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgChallengeContent{
				Challenger:  ma.addr(),
				TargetType:  commontypes.Saturate[uint64](b.TargetType),
				TargetId:    b.TargetId,
				Reason:      randomReason(e.r),
				Evidence:    []string{randomURI(e.r)},
				StakedDream: PtrInt(randBetween(e.r, floor, math.MaxInt(floor, unlocked(ma.m).QuoRaw(4)))),
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no member can challenge author-bonded content")
	})
}

// SimulateMsgRespondToContentChallenge has the author of challenged content
// answer an ACTIVE content challenge: usually with a defense (a jury is
// seated), sometimes with an empty response (the challenge is upheld).
func SimulateMsgRespondToContentChallenge(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRespondToContentChallenge{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ccs, err := e.contentChallenges(func(cc types.ContentChallenge) bool {
			_, ok := e.account(cc.Author)
			return ok && cc.Status == types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE
		})
		if err != nil {
			return noOpErr(label, err)
		}
		cands := make([]candidate, 0, len(ccs))
		for _, cc := range ccs {
			acc, _ := e.account(cc.Author)
			msg := &types.MsgRespondToContentChallenge{Author: cc.Author, ContentChallengeId: cc.Id}
			if e.r.Intn(6) != 0 {
				msg.Response = randomReason(e.r)
				msg.Evidence = []string{randomURI(e.r)}
			}
			cands = append(cands, candidate{acc: acc, msg: msg, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no ACTIVE content challenge with a live author")
	})
}
