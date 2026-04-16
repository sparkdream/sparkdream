package keeper

import (
	"context"
	"strconv"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlockProposals expires proposals that have passed their voting deadline
// and tallies their votes to determine acceptance or rejection.
func (k Keeper) EndBlockProposals(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	var toFinalize []uint64

	// Walk all proposals to find ones past their voting deadline that are still open.
	// Skip proposals in terminal states (EXECUTED, REJECTED, EXPIRED, FAILED, VETOED)
	// to avoid unbounded linear growth in scanning time.
	err := k.Proposals.Walk(ctx, nil, func(id uint64, proposal types.Proposal) (bool, error) {
		switch proposal.Status {
		case types.ProposalStatus_PROPOSAL_STATUS_EXECUTED,
			types.ProposalStatus_PROPOSAL_STATUS_REJECTED,
			types.ProposalStatus_PROPOSAL_STATUS_EXPIRED,
			types.ProposalStatus_PROPOSAL_STATUS_FAILED,
			types.ProposalStatus_PROPOSAL_STATUS_VETOED:
			return false, nil // skip finalized proposals
		}
		if proposal.Status == types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED && now > proposal.VotingDeadline {
			toFinalize = append(toFinalize, id)
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, proposalID := range toFinalize {
		proposal, err := k.Proposals.Get(ctx, proposalID)
		if err != nil {
			continue
		}

		// Check if threshold is met
		accepted, err := k.checkThreshold(ctx, proposal)
		if err != nil {
			// If we can't tally, reject the proposal
			proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_REJECTED
			proposal.FailedReason = "failed to tally votes: " + err.Error()
			_ = k.Proposals.Set(ctx, proposalID, proposal)
			continue
		}

		if accepted {
			proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED
		} else {
			proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_REJECTED
			proposal.FailedReason = "threshold not met at voting deadline"
		}

		if err := k.Proposals.Set(ctx, proposalID, proposal); err != nil {
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
func (k Keeper) TallyProposal(ctx context.Context, proposal types.Proposal) (types.TallyResult, error) {
	var yesWeight, noWeight, abstainWeight math.LegacyDec
	yesWeight = math.LegacyZeroDec()
	noWeight = math.LegacyZeroDec()
	abstainWeight = math.LegacyZeroDec()

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
		}
		return false, nil
	})
	if err != nil {
		return types.TallyResult{}, err
	}

	return types.TallyResult{
		YesWeight:     yesWeight.String(),
		NoWeight:      noWeight.String(),
		AbstainWeight: abstainWeight.String(),
	}, nil
}
