package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vote/types"
)

// ProcessEndBlock runs end-of-block proposal lifecycle transitions.
func (k Keeper) ProcessEndBlock(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// TLE liveness tracking (runs cheap Has check every block, full check only on epoch boundary).
	if err := k.trackTleLiveness(ctx); err != nil {
		// Log but don't fail consensus -- liveness tracking is observability-only.
		sdkCtx.Logger().Error("TLE liveness tracking failed", "error", err)
	}

	return k.VotingProposal.Walk(ctx, nil, func(id uint64, proposal types.VotingProposal) (bool, error) {
		switch proposal.Status {
		case types.ProposalStatus_PROPOSAL_STATUS_ACTIVE:
			if blockHeight >= proposal.VotingEnd {
				if proposal.Visibility == types.VisibilityLevel_VISIBILITY_PUBLIC {
					// PUBLIC proposals finalize immediately after voting ends.
					if err := k.finalizeProposal(ctx, &proposal); err != nil {
						return false, err
					}
				} else {
					// SEALED/PRIVATE: transition to TALLYING for reveal phase.
					proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_TALLYING
					if err := k.VotingProposal.Set(ctx, id, proposal); err != nil {
						return false, err
					}
				}
			}

		case types.ProposalStatus_PROPOSAL_STATUS_TALLYING:
			// Try auto-reveal sealed votes if epoch decryption key is available.
			k.tryAutoReveal(ctx, &proposal)

			// Finalize if reveal period has ended.
			if blockHeight >= proposal.RevealEnd {
				if err := k.finalizeProposal(ctx, &proposal); err != nil {
					return false, err
				}
			}
		}

		return false, nil // continue walking
	})
}

// tryAutoReveal attempts to decrypt and reveal sealed votes using TLE decryption key.
func (k Keeper) tryAutoReveal(ctx context.Context, proposal *types.VotingProposal) {
	if proposal.RevealEpoch == 0 {
		return
	}

	// Check if decryption key is available for the reveal epoch.
	epochKey, err := k.EpochDecryptionKey.Get(ctx, proposal.RevealEpoch)
	if err != nil || len(epochKey.DecryptionKey) == 0 {
		return // key not yet available
	}

	// Walk sealed votes for this proposal and attempt decryption.
	prefix := fmt.Sprintf("%d/", proposal.Id)
	_ = k.SealedVote.Walk(ctx, nil, func(key string, sv types.SealedVote) (bool, error) {
		// Only process votes for this proposal that haven't been revealed.
		if sv.ProposalId != proposal.Id || sv.Revealed || len(key) < len(prefix) || key[:len(prefix)] != prefix {
			return false, nil
		}

		if len(sv.EncryptedReveal) == 0 {
			return false, nil
		}

		// Attempt decryption (stub).
		_, voteOption, salt, err := decryptTLEPayload(sv.EncryptedReveal, epochKey.DecryptionKey)
		if err != nil {
			return false, nil // skip failed decryptions
		}

		// Verify commitment.
		expectedCommitment := computeCommitmentHash(voteOption, salt)
		if !bytesEqual(expectedCommitment, sv.VoteCommitment) {
			return false, nil // commitment mismatch, skip
		}

		// Validate option.
		if voteOption >= uint32(len(proposal.Options)) {
			return false, nil
		}

		// Mark revealed and update tally.
		sv.Revealed = true
		sv.RevealedOption = voteOption
		sv.RevealSalt = salt
		if err := k.SealedVote.Set(ctx, key, sv); err != nil {
			return false, nil
		}

		// Update tally directly on the proposal struct (we'll save at the end).
		for i, t := range proposal.Tally {
			if t.OptionId == voteOption {
				proposal.Tally[i].VoteCount++
				break
			}
		}

		return false, nil
	})

	// Save updated proposal with tally changes.
	_ = k.VotingProposal.Set(ctx, proposal.Id, *proposal)
}

// finalizeProposal performs role-aware tallying and sets the final outcome.
func (k Keeper) finalizeProposal(ctx context.Context, proposal *types.VotingProposal) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Count votes by role.
	var totalSubmitted, abstainVotes, vetoVotes uint64
	for _, t := range proposal.Tally {
		totalSubmitted += t.VoteCount
	}

	// Build role map from options.
	optionRoles := make(map[uint32]types.OptionRole)
	for _, opt := range proposal.Options {
		optionRoles[opt.Id] = opt.Role
	}

	for _, t := range proposal.Tally {
		switch optionRoles[t.OptionId] {
		case types.OptionRole_OPTION_ROLE_ABSTAIN:
			abstainVotes += t.VoteCount
		case types.OptionRole_OPTION_ROLE_VETO:
			vetoVotes += t.VoteCount
		}
	}

	outcome := types.ProposalOutcome_PROPOSAL_OUTCOME_REJECTED

	// 1. Quorum check.
	if proposal.EligibleVoters > 0 {
		quorumRatio := math.LegacyNewDec(int64(totalSubmitted)).Quo(math.LegacyNewDec(int64(proposal.EligibleVoters)))
		if quorumRatio.LT(proposal.Quorum) {
			outcome = types.ProposalOutcome_PROPOSAL_OUTCOME_QUORUM_NOT_MET
			return k.setFinalOutcome(ctx, proposal, outcome, sdkCtx.BlockHeight())
		}
	} else {
		outcome = types.ProposalOutcome_PROPOSAL_OUTCOME_QUORUM_NOT_MET
		return k.setFinalOutcome(ctx, proposal, outcome, sdkCtx.BlockHeight())
	}

	// 2. Non-abstain count.
	nonAbstain := totalSubmitted - abstainVotes
	if nonAbstain == 0 {
		outcome = types.ProposalOutcome_PROPOSAL_OUTCOME_QUORUM_NOT_MET
		return k.setFinalOutcome(ctx, proposal, outcome, sdkCtx.BlockHeight())
	}

	// 3. Veto check.
	vetoRatio := math.LegacyNewDec(int64(vetoVotes)).Quo(math.LegacyNewDec(int64(nonAbstain)))
	if vetoRatio.GT(proposal.VetoThreshold) {
		outcome = types.ProposalOutcome_PROPOSAL_OUTCOME_VETOED
		return k.setFinalOutcome(ctx, proposal, outcome, sdkCtx.BlockHeight())
	}

	// 4. Threshold check: find winning standard option.
	var winningOptionID uint32
	var winningVotes uint64
	for _, t := range proposal.Tally {
		if optionRoles[t.OptionId] == types.OptionRole_OPTION_ROLE_STANDARD {
			if t.VoteCount > winningVotes || (t.VoteCount == winningVotes && t.OptionId < winningOptionID) {
				winningVotes = t.VoteCount
				winningOptionID = t.OptionId
			}
		}
	}

	winRatio := math.LegacyNewDec(int64(winningVotes)).Quo(math.LegacyNewDec(int64(nonAbstain)))
	if winRatio.GT(proposal.Threshold) {
		outcome = types.ProposalOutcome_PROPOSAL_OUTCOME_PASSED
	}

	return k.setFinalOutcome(ctx, proposal, outcome, sdkCtx.BlockHeight())
}

// setFinalOutcome updates proposal status, handles deposits, and emits events.
func (k Keeper) setFinalOutcome(ctx context.Context, proposal *types.VotingProposal, outcome types.ProposalOutcome, blockHeight int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FINALIZED
	proposal.Outcome = outcome
	proposal.FinalizedAt = blockHeight

	if err := k.VotingProposal.Set(ctx, proposal.Id, *proposal); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventProposalFinalized,
		sdk.NewAttribute(types.AttributeProposalID, fmt.Sprintf("%d", proposal.Id)),
		sdk.NewAttribute(types.AttributeOutcome, outcome.String()),
		sdk.NewAttribute(types.AttributeTotalVotes, fmt.Sprintf("%d", k.totalVotes(proposal))),
	))

	return nil
}

// totalVotes sums all tally vote counts.
func (k Keeper) totalVotes(proposal *types.VotingProposal) uint64 {
	var total uint64
	for _, t := range proposal.Tally {
		total += t.VoteCount
	}
	return total
}
