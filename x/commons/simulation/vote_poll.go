package simulation

import (
	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgVotePoll casts an open vote on an open, openly voted poll
// before its deadline, from a live seat of its group that has not voted yet
// (the dry-run applies the alias-aware one-vote rule).
func SimulateMsgVotePoll(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgVotePoll{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		polls, err := e.openPolls(func(p types.Poll) bool {
			return !p.SecretBallot && e.ctx.BlockTime().Unix() <= p.Deadline
		})
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(polls)) {
			p := polls[i]
			seats, err := e.liveSeatAccounts(p.CouncilName)
			if err != nil {
				return noOpErr(label, err)
			}
			for _, j := range e.r.Perm(len(seats)) {
				voter := seats[j]
				voted, err := e.k.PollVotes.Has(e.ctx, collections.Join(p.Id, voter.Address.String()))
				if err != nil {
					return noOpErr(label, err)
				}
				if voted {
					continue
				}
				msg := &types.MsgVotePoll{
					Voter:  voter.Address.String(),
					PollId: p.Id,
					Option: commontypes.Saturate[uint64](e.r.Intn(len(p.Options))),
				}
				if e.dryRun(msg) != nil {
					continue
				}
				return e.submit(voter, msg)
			}
		}
		return noOp(label, "no open poll a live seat can still vote on")
	})
}

// openPolls returns the open polls accepted by keep.
func (e *simEnv) openPolls(keep func(types.Poll) bool) ([]types.Poll, error) {
	var out []types.Poll
	err := e.k.OpenPollsByCouncil.Walk(e.ctx, nil, func(key collections.Pair[string, uint64]) (bool, error) {
		p, err := e.k.Polls.Get(e.ctx, key.K2())
		if err != nil {
			return true, err
		}
		if p.Status == types.PollStatus_POLL_STATUS_OPEN && keep(p) {
			out = append(out, p)
		}
		return false, nil
	})
	return out, err
}
