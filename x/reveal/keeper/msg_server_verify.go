package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

func (k msgServer) Verify(ctx context.Context, msg *types.MsgVerify) (*types.MsgVerifyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Voter); err != nil {
		return nil, types.ErrNotMember.Wrapf("invalid voter address: %s", err)
	}

	// Validate quality rating
	if msg.QualityRating < 1 || msg.QualityRating > 5 {
		return nil, types.ErrInvalidQualityRating.Wrapf("got %d", msg.QualityRating)
	}

	// Get contribution
	contrib, err := k.Contribution.Get(ctx, msg.ContributionId)
	if err != nil {
		return nil, types.ErrContributionNotFound.Wrapf("contribution %d", msg.ContributionId)
	}

	// Must be IN_PROGRESS
	if contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
		return nil, types.ErrNotInProgress
	}

	// Self-vote prevention: contributor cannot vote on own contribution
	if msg.Voter == contrib.Contributor {
		return nil, types.ErrSelfVote
	}

	// Get tranche
	tranche, err := GetTranche(&contrib, msg.TrancheId)
	if err != nil {
		return nil, err
	}

	// Tranche must be REVEALED
	if tranche.Status != types.TrancheStatus_TRANCHE_STATUS_REVEALED {
		return nil, types.ErrTrancheNotRevealed
	}

	// Check voter hasn't already voted
	vk := VoteKey(msg.ContributionId, msg.TrancheId, msg.Voter)
	hasVoted, err := k.Vote.Has(ctx, vk)
	if err != nil {
		return nil, err
	}
	if hasVoted {
		return nil, types.ErrAlreadyVoted
	}

	// Voter must be a staker for this tranche (skin in the game)
	stakeWeight := math.ZeroInt()
	trancheKey := TrancheKey(msg.ContributionId, msg.TrancheId)
	err = k.StakesByTranche.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](trancheKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			s, err := k.RevealStake.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			if s.Staker == msg.Voter {
				stakeWeight = stakeWeight.Add(s.Amount)
			}
			return false, nil
		},
	)
	if err != nil {
		return nil, err
	}
	if stakeWeight.IsZero() {
		return nil, types.ErrNotStaker
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()

	vote := types.VerificationVote{
		Voter:          msg.Voter,
		ContributionId: msg.ContributionId,
		TrancheId:      msg.TrancheId,
		ValueConfirmed: msg.ValueConfirmed,
		QualityRating:  msg.QualityRating,
		Comments:       msg.Comments,
		StakeWeight:    stakeWeight,
		VotedAt:        currentEpoch,
	}

	// Save vote and indexes
	if err := k.Vote.Set(ctx, vk, vote); err != nil {
		return nil, err
	}
	if err := k.VotesByTranche.Set(ctx, collections.Join(trancheKey, vk)); err != nil {
		return nil, err
	}
	if err := k.VotesByVoter.Set(ctx, collections.Join(msg.Voter, vk)); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"verification_vote",
			sdk.NewAttribute("voter", msg.Voter),
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", msg.ContributionId)),
			sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", msg.TrancheId)),
			sdk.NewAttribute("value_confirmed", fmt.Sprintf("%t", msg.ValueConfirmed)),
			sdk.NewAttribute("quality_rating", fmt.Sprintf("%d", msg.QualityRating)),
			sdk.NewAttribute("stake_weight", stakeWeight.String()),
		),
	)

	return &types.MsgVerifyResponse{}, nil
}
