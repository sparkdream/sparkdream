package keeper

import (
	"context"
	commontypes "sparkdream/x/common/types"
	"strconv"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxProposalTransitionsPerBlock caps the ProposalQueue entries EndBlock
// processes per block (voting-deadline tallies and execution-window
// expiries together, stale entries included). A tally walks every vote of
// the proposal outside transaction gas metering, so a burst of proposals
// sharing a deadline (a backlog, or anonymous proposals across councils at
// MaxAnonymousProposalsPerCouncilPerDay each) must not land in one block.
// The rest stays queued and drains in (due_time, proposal_id) order over
// the next blocks; a few blocks of delay on a finalisation is harmless,
// since voting is closed by the deadline and execution checks the status.
// Twice MaxMarketTriggersPerBlock because a transition is cheaper than
// opening a market.
const MaxProposalTransitionsPerBlock = 20

// EndBlockProposals finalizes proposals whose voting deadline has passed
// (tally -> ACCEPTED or REJECTED) and expires ACCEPTED proposals whose
// execution window has closed. It walks only the ProposalQueue entries that
// are due, at most MaxProposalTransitionsPerBlock of them, so the cost per
// block does not grow with proposal history or with a deadline backlog.
func (k Keeper) EndBlockProposals(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Due entries: due_time < now (strict, matching "now > deadline"),
	// oldest first, capped per block.
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now, uint64(0)))
	var due []collections.Pair[int64, uint64]
	if err := k.ProposalQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		due = append(due, key)
		return len(due) >= MaxProposalTransitionsPerBlock, nil
	}); err != nil {
		return err
	}

	for _, key := range due {
		if err := k.ProposalQueue.Remove(ctx, key); err != nil {
			return err
		}
		proposalID := key.K2()
		proposal, err := k.Proposals.Get(ctx, proposalID)
		if err != nil {
			continue
		}
		// Voting is over for a proposal that left SUBMITTED (accepted early,
		// vetoed, cancelled): its secret-ballot voter snapshot can go. The
		// snapshot of one that just reached its deadline goes below.
		if proposal.Status != types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED {
			k.dropProposalVoterTree(ctx, proposalID)
		}
		// A stale entry (the proposal moved on since it was queued) is
		// simply dropped; the current entry, if any, is elsewhere.
		if dueAt, ok := queueDueTime(proposal); !ok || dueAt != key.K1() {
			continue
		}

		switch proposal.Status {
		case types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED:
			accepted, err := k.checkThreshold(ctx, proposal)
			switch {
			case err != nil:
				// If we can't tally, reject the proposal
				proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_REJECTED
				proposal.FailedReason = "failed to tally votes: " + err.Error()
			case accepted:
				proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED
			default:
				proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_REJECTED
				proposal.FailedReason = "threshold not met at voting deadline"
			}
			k.dropProposalVoterTree(ctx, proposalID)
		case types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED:
			proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_EXPIRED
			proposal.FailedReason = "execution window closed"
		default:
			continue
		}

		if err := k.Proposals.Set(ctx, proposalID, proposal); err != nil {
			return err
		}
		if err := k.enqueueProposal(ctx, proposal); err != nil {
			return err
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"proposal_finalized",
				sdk.NewAttribute("proposal_id", strconv.FormatUint(proposalID, 10)),
				sdk.NewAttribute("status", proposal.Status.String()),
			),
		)
	}

	return nil
}

// TallyProposal computes the tally result for a proposal without modifying state.
// A secret ballot reports its anonymous counts at the snapshot's member weight.
func (k Keeper) TallyProposal(ctx context.Context, proposal types.Proposal) (types.TallyResult, error) {
	if proposal.SecretBallot {
		w, err := math.LegacyNewDecFromStr(proposal.BallotMemberWeight)
		if err != nil {
			return types.TallyResult{}, err
		}
		tally, err := k.AnonVoteTallies.Get(ctx, proposal.Id)
		if err != nil {
			tally = types.AnonVoteTally{}
		}
		return types.TallyResult{
			YesWeight:        w.MulInt64(commontypes.Saturate[int64](tally.YesCount)).String(),
			NoWeight:         w.MulInt64(commontypes.Saturate[int64](tally.NoCount)).String(),
			AbstainWeight:    w.MulInt64(commontypes.Saturate[int64](tally.AbstainCount)).String(),
			NoWithVetoWeight: w.MulInt64(commontypes.Saturate[int64](tally.NoWithVetoCount)).String(),
		}, nil
	}

	var yesWeight, noWeight, abstainWeight, vetoWeight math.LegacyDec
	yesWeight = math.LegacyZeroDec()
	noWeight = math.LegacyZeroDec()
	abstainWeight = math.LegacyZeroDec()
	vetoWeight = math.LegacyZeroDec()

	rng := collections.NewPrefixedPairRange[uint64, string](proposal.Id)
	err := k.Votes.Walk(ctx, rng, func(_ collections.Pair[uint64, string], vote types.Vote) (bool, error) {
		member, err := k.GetMember(ctx, proposal.CouncilName, vote.Voter)
		if err != nil {
			return false, nil // Member may have been removed
		}

		weight, err := math.LegacyNewDecFromStr(member.Weight)
		if err != nil {
			return false, nil
		}

		switch vote.Option {
		case types.VoteOption_VOTE_OPTION_YES:
			yesWeight = yesWeight.Add(weight)
		case types.VoteOption_VOTE_OPTION_NO:
			noWeight = noWeight.Add(weight)
		case types.VoteOption_VOTE_OPTION_ABSTAIN:
			abstainWeight = abstainWeight.Add(weight)
		case types.VoteOption_VOTE_OPTION_NO_WITH_VETO:
			// Reported for transparency; like NO it simply withholds a YES
			// (commons councils have no veto quorum, spec §6.3).
			vetoWeight = vetoWeight.Add(weight)
		}
		return false, nil
	})
	if err != nil {
		return types.TallyResult{}, err
	}

	return types.TallyResult{
		YesWeight:        yesWeight.String(),
		NoWeight:         noWeight.String(),
		AbstainWeight:    abstainWeight.String(),
		NoWithVetoWeight: vetoWeight.String(),
	}, nil
}
