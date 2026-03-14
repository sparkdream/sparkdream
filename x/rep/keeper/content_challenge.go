package keeper

import (
	"context"
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// CreateContentChallenge creates a new challenge against bonded content.
// Any member can challenge content that has an author bond.
func (k Keeper) CreateContentChallenge(
	ctx context.Context,
	challengerAddr sdk.AccAddress,
	targetType types.StakeTargetType,
	targetID uint64,
	reason string,
	evidence []string,
	stakedDream math.Int,
) (uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	// Validate target type is an author bond type
	if !types.IsAuthorBondType(targetType) {
		return 0, types.ErrNotAuthorBondType
	}

	// Get the author bond — fail if none exists
	bond, err := k.GetAuthorBond(ctx, targetType, targetID)
	if err != nil {
		return 0, types.ErrNoAuthorBond
	}

	// Cannot challenge your own content
	if bond.Staker == challengerAddr.String() {
		return 0, types.ErrCannotChallengeOwnContent
	}

	// Verify challenger is a member
	_, err = k.GetMember(ctx, challengerAddr)
	if err != nil {
		return 0, types.ErrNotMember
	}

	// Check no active challenge on this content
	targetKey := collections.Join(int32(targetType), targetID)
	_, err = k.ContentChallengesByTarget.Get(ctx, targetKey)
	if err == nil {
		return 0, types.ErrContentChallengeExists
	}

	// Validate stake amount
	minStake := params.MinChallengeStake
	if stakedDream.LT(minStake) {
		return 0, fmt.Errorf("insufficient stake: %s, required: %s", stakedDream, minStake)
	}

	// Lock challenger's DREAM
	if err := k.LockDREAM(ctx, challengerAddr, stakedDream); err != nil {
		return 0, fmt.Errorf("failed to lock DREAM for content challenge: %w", err)
	}

	// Get next content challenge ID
	ccID, err := k.ContentChallengeSeq.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next content challenge ID: %w", err)
	}

	// Calculate response deadline
	responseDeadline := sdkCtx.BlockHeight() + (params.ChallengeResponseDeadlineEpochs * params.EpochBlocks)

	// Resolve author address from bond
	authorAddr := bond.Staker

	// Create content challenge
	cc := types.ContentChallenge{
		Id:               ccID,
		TargetType:       targetType,
		TargetId:         targetID,
		Challenger:       challengerAddr.String(),
		Reason:           reason,
		Evidence:         evidence,
		StakedDream:      stakedDream,
		Author:           authorAddr,
		Status:           types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE,
		CreatedAt:        sdkCtx.BlockHeight(),
		ResolvedAt:       0,
		ResponseDeadline: responseDeadline,
		JuryReviewId:     0,
		AuthorResponse:   "",
		AuthorEvidence:   []string{},
		BondAmount:       bond.Amount,
	}

	// Store content challenge
	if err := k.ContentChallenge.Set(ctx, ccID, cc); err != nil {
		return 0, fmt.Errorf("failed to store content challenge: %w", err)
	}

	// Add to status index
	if err := k.AddContentChallengeToStatusIndex(ctx, cc); err != nil {
		return 0, fmt.Errorf("failed to add content challenge to status index: %w", err)
	}

	// Add to target index (enforces one active challenge per content item)
	if err := k.ContentChallengesByTarget.Set(ctx, targetKey, ccID); err != nil {
		return 0, fmt.Errorf("failed to add content challenge to target index: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"content_challenge_created",
			sdk.NewAttribute("content_challenge_id", fmt.Sprintf("%d", ccID)),
			sdk.NewAttribute("target_type", targetType.String()),
			sdk.NewAttribute("target_id", fmt.Sprintf("%d", targetID)),
			sdk.NewAttribute("challenger", challengerAddr.String()),
			sdk.NewAttribute("author", authorAddr),
			sdk.NewAttribute("staked_dream", stakedDream.String()),
			sdk.NewAttribute("bond_amount", bond.Amount.String()),
		),
	)

	return ccID, nil
}

// RespondToContentChallenge allows the content author to respond to a challenge.
// If the response is empty, the challenge is auto-upheld (forfeit).
// Otherwise, a jury review is created.
func (k Keeper) RespondToContentChallenge(
	ctx context.Context,
	ccID uint64,
	authorAddr sdk.AccAddress,
	response string,
	evidence []string,
) error {
	cc, err := k.ContentChallenge.Get(ctx, ccID)
	if err != nil {
		return types.ErrContentChallengeNotFound
	}

	// Validate status
	if cc.Status != types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE {
		return types.ErrContentChallengeNotActive
	}

	// Verify the responder is the author
	if cc.Author != authorAddr.String() {
		return types.ErrNotContentAuthor
	}

	// Store the response on the challenge
	cc.AuthorResponse = response
	cc.AuthorEvidence = evidence
	if err := k.ContentChallenge.Set(ctx, ccID, cc); err != nil {
		return err
	}

	// Empty response = forfeit → auto-uphold
	if response == "" {
		return k.UpholdContentChallenge(ctx, ccID)
	}

	// Non-empty response → route to jury
	return k.CreateContentJuryReview(ctx, ccID)
}

// UpholdContentChallenge upholds a content challenge: slashes author bond, rewards challenger.
func (k Keeper) UpholdContentChallenge(ctx context.Context, ccID uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	cc, err := k.ContentChallenge.Get(ctx, ccID)
	if err != nil {
		return types.ErrContentChallengeNotFound
	}

	// Get author address
	authorAddr, err := sdk.AccAddressFromBech32(cc.Author)
	if err != nil {
		return fmt.Errorf("invalid author address: %w", err)
	}

	// Slash the author bond: unlock → burn (same pattern as SlashAuthorBond)
	bondAmount := cc.BondAmount
	if bondAmount.IsPositive() {
		if err := k.UnlockDREAM(ctx, authorAddr, bondAmount); err != nil {
			// Bond might already be gone; log and continue
			sdkCtx.Logger().Debug("failed to unlock author bond for slashing", "error", err)
		} else {
			if err := k.BurnDREAM(ctx, authorAddr, bondAmount); err != nil {
				return fmt.Errorf("failed to burn author bond: %w", err)
			}
		}

		// Remove the bond stake from storage
		bond, bondErr := k.GetAuthorBond(ctx, cc.TargetType, cc.TargetId)
		if bondErr == nil {
			_ = k.RemoveStakeFromTargetIndex(ctx, bond)
			_ = k.Stake.Remove(ctx, bond.Id)
		}
	}

	// Calculate challenger reward
	rewardShare := params.ContentChallengeRewardShare
	rewardAmount := bondAmount.ToLegacyDec().Mul(rewardShare).TruncateInt()

	// Get challenger address
	challengerAddr, err := sdk.AccAddressFromBech32(cc.Challenger)
	if err != nil {
		return fmt.Errorf("invalid challenger address: %w", err)
	}

	// Mint reward to challenger
	if rewardAmount.IsPositive() {
		if err := k.MintDREAM(ctx, challengerAddr, rewardAmount); err != nil {
			return fmt.Errorf("failed to mint reward to challenger: %w", err)
		}
	}

	// Return challenger's staked DREAM
	if cc.StakedDream.IsPositive() {
		if err := k.UnlockDREAM(ctx, challengerAddr, cc.StakedDream); err != nil {
			return fmt.Errorf("failed to return challenger stake: %w", err)
		}
	}

	// Update content challenge status
	oldStatus := cc.Status
	cc.Status = types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_UPHELD
	cc.ResolvedAt = sdkCtx.BlockHeight()
	if err := k.ContentChallenge.Set(ctx, ccID, cc); err != nil {
		return err
	}

	// Update indexes
	_ = k.UpdateContentChallengeStatusIndex(ctx, oldStatus, cc.Status, ccID)
	_ = k.ContentChallengesByTarget.Remove(ctx, collections.Join(int32(cc.TargetType), cc.TargetId))

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"content_challenge_upheld",
			sdk.NewAttribute("content_challenge_id", fmt.Sprintf("%d", ccID)),
			sdk.NewAttribute("bond_slashed", bondAmount.String()),
			sdk.NewAttribute("challenger_reward", rewardAmount.String()),
		),
	)

	return nil
}

// RejectContentChallenge rejects a content challenge: burns challenger's stake.
func (k Keeper) RejectContentChallenge(ctx context.Context, ccID uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	cc, err := k.ContentChallenge.Get(ctx, ccID)
	if err != nil {
		return types.ErrContentChallengeNotFound
	}

	// Burn challenger's staked DREAM
	challengerAddr, err := sdk.AccAddressFromBech32(cc.Challenger)
	if err != nil {
		return fmt.Errorf("invalid challenger address: %w", err)
	}

	if cc.StakedDream.IsPositive() {
		if err := k.UnlockDREAM(ctx, challengerAddr, cc.StakedDream); err != nil {
			return fmt.Errorf("failed to unlock challenger stake for burning: %w", err)
		}
		if err := k.BurnDREAM(ctx, challengerAddr, cc.StakedDream); err != nil {
			return fmt.Errorf("failed to burn challenger stake: %w", err)
		}
	}

	// Update status
	oldStatus := cc.Status
	cc.Status = types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_REJECTED
	cc.ResolvedAt = sdkCtx.BlockHeight()
	if err := k.ContentChallenge.Set(ctx, ccID, cc); err != nil {
		return err
	}

	// Update indexes
	_ = k.UpdateContentChallengeStatusIndex(ctx, oldStatus, cc.Status, ccID)
	_ = k.ContentChallengesByTarget.Remove(ctx, collections.Join(int32(cc.TargetType), cc.TargetId))

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"content_challenge_rejected",
			sdk.NewAttribute("content_challenge_id", fmt.Sprintf("%d", ccID)),
			sdk.NewAttribute("challenger_stake_burned", cc.StakedDream.String()),
		),
	)

	return nil
}

// ResolveInconclusiveContentChallenge resolves an inconclusive jury verdict for a content challenge.
// Status quo preserved: challenger gets stake back, bond remains.
func (k Keeper) ResolveInconclusiveContentChallenge(ctx context.Context, ccID uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	cc, err := k.ContentChallenge.Get(ctx, ccID)
	if err != nil {
		return types.ErrContentChallengeNotFound
	}

	// Return challenger's staked DREAM (no penalty for inconclusive)
	challengerAddr, err := sdk.AccAddressFromBech32(cc.Challenger)
	if err != nil {
		return fmt.Errorf("invalid challenger address: %w", err)
	}

	if cc.StakedDream.IsPositive() {
		if err := k.UnlockDREAM(ctx, challengerAddr, cc.StakedDream); err != nil {
			return fmt.Errorf("failed to return challenger stake: %w", err)
		}
	}

	// Update status to REJECTED (status quo preserved)
	oldStatus := cc.Status
	cc.Status = types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_REJECTED
	cc.ResolvedAt = sdkCtx.BlockHeight()
	if err := k.ContentChallenge.Set(ctx, ccID, cc); err != nil {
		return err
	}

	// Update indexes
	_ = k.UpdateContentChallengeStatusIndex(ctx, oldStatus, cc.Status, ccID)
	_ = k.ContentChallengesByTarget.Remove(ctx, collections.Join(int32(cc.TargetType), cc.TargetId))

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"content_challenge_inconclusive",
			sdk.NewAttribute("content_challenge_id", fmt.Sprintf("%d", ccID)),
		),
	)

	return nil
}

// HasActiveContentChallenge checks if there is an active content challenge for a target.
func (k Keeper) HasActiveContentChallenge(ctx context.Context, targetType types.StakeTargetType, targetID uint64) (bool, error) {
	targetKey := collections.Join(int32(targetType), targetID)
	_, err := k.ContentChallengesByTarget.Get(ctx, targetKey)
	if err != nil {
		return false, nil // Not found = no active challenge
	}
	return true, nil
}

// CreateContentJuryReview creates a jury review for a content challenge.
// Uses general reputation (any tag) for juror selection since content may lack domain tags.
func (k Keeper) CreateContentJuryReview(ctx context.Context, ccID uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	cc, err := k.ContentChallenge.Get(ctx, ccID)
	if err != nil {
		return types.ErrContentChallengeNotFound
	}

	// Select jurors (excluding challenger and author)
	jurors, err := k.SelectContentJury(ctx, params.JurySize, cc.Challenger, cc.Author)
	if err != nil {
		return fmt.Errorf("failed to select content jury: %w", err)
	}

	// Calculate required votes (supermajority)
	superMajority := params.JurySuperMajority
	requiredVotes := superMajority.MulInt64(int64(len(jurors))).Ceil().TruncateInt().Uint64()

	// Get next jury review ID
	juryReviewID, err := k.JuryReviewSeq.Next(ctx)
	if err != nil {
		return err
	}

	// Create jury review with content_challenge_id set
	juryReview := types.JuryReview{
		Id:                 juryReviewID,
		ChallengeId:        0, // Not an initiative challenge
		InitiativeId:       0, // Not an initiative
		ContentChallengeId: ccID,
		Jurors:             jurors,
		RequiredVotes:      uint32(requiredVotes),
		ExpertWitnesses:    []string{},
		Testimonies:        []*types.ExpertTestimony{},
		ReviewDeliverable:  fmt.Sprintf("Content challenge %d against %s #%d", ccID, cc.TargetType, cc.TargetId),
		ChallengerClaim:    cc.Reason,
		AssigneeResponse:   cc.AuthorResponse,
		Votes:              []*types.JurorVote{},
		Deadline:           sdkCtx.BlockHeight() + params.DefaultReviewPeriodEpochs*params.EpochBlocks,
		Verdict:            types.Verdict_VERDICT_PENDING,
	}

	// Save jury review
	if err := k.JuryReview.Set(ctx, juryReview.Id, juryReview); err != nil {
		return err
	}

	// Update content challenge status
	oldStatus := cc.Status
	cc.Status = types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_IN_JURY_REVIEW
	cc.JuryReviewId = juryReviewID
	cc.ResponseDeadline = 0 // Clear deadline since it's now in jury review
	if err := k.ContentChallenge.Set(ctx, ccID, cc); err != nil {
		return err
	}

	// Update status index
	_ = k.UpdateContentChallengeStatusIndex(ctx, oldStatus, cc.Status, ccID)

	// Create JURY_DUTY interims for jurors
	for _, jurorAddr := range jurors {
		_, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_JURY_DUTY,
			[]string{jurorAddr},
			"",
			0, // No initiative reference
			fmt.Sprintf("content_challenge:%d", ccID),
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			juryReview.Deadline,
		)
		if err != nil {
			return err
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"content_jury_review_created",
			sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", juryReviewID)),
			sdk.NewAttribute("content_challenge_id", fmt.Sprintf("%d", ccID)),
			sdk.NewAttribute("juror_count", fmt.Sprintf("%d", len(jurors))),
		),
	)

	return nil
}

// SelectContentJury selects jury members for a content challenge.
// Unlike initiative jury selection, this uses overall reputation (any tag >= MinJurorReputation)
// since content challenges may not have domain-specific tags.
func (k Keeper) SelectContentJury(
	ctx context.Context,
	jurySize uint32,
	excludeAddrs ...string,
) ([]string, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Build exclude set
	excludeSet := make(map[string]bool)
	for _, addr := range excludeAddrs {
		excludeSet[addr] = true
	}

	minReputation := params.MinJurorReputation

	// Get all eligible members
	type eligibleMember struct {
		address  string
		totalRep math.LegacyDec
	}
	var eligible []eligibleMember

	err = k.Member.Walk(ctx, nil, func(addr string, member types.Member) (stop bool, err error) {
		// Skip excluded addresses
		if excludeSet[addr] {
			return false, nil
		}

		// Check if any reputation tag meets minimum
		totalRep := math.LegacyZeroDec()
		hasMinRep := false
		for _, scoreStr := range member.ReputationScores {
			score, err := math.LegacyNewDecFromStr(scoreStr)
			if err != nil {
				continue
			}
			totalRep = totalRep.Add(score)
			if score.GTE(minReputation) {
				hasMinRep = true
			}
		}

		if hasMinRep {
			eligible = append(eligible, eligibleMember{
				address:  addr,
				totalRep: totalRep,
			})
		}

		return false, nil
	})
	if err != nil {
		return nil, err
	}

	if len(eligible) < int(jurySize) {
		return nil, fmt.Errorf("insufficient eligible jurors: need %d, have %d", jurySize, len(eligible))
	}

	// Weighted random selection based on total reputation
	weights := make([]float64, len(eligible))
	for i, m := range eligible {
		weights[i] = m.totalRep.MustFloat64()
	}

	selectedJurors := make([]string, 0, jurySize)
	for i := 0; i < int(jurySize); i++ {
		selected := contentWeightedRandomSelect(weights)
		selectedJurors = append(selectedJurors, eligible[selected].address)

		// Remove selected from pool
		eligible = append(eligible[:selected], eligible[selected+1:]...)
		weights = append(weights[:selected], weights[selected+1:]...)
	}

	return selectedJurors, nil
}

// contentWeightedRandomSelect performs weighted random selection (same as weightedRandomSelect)
func contentWeightedRandomSelect(weights []float64) int {
	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}

	if totalWeight <= 0 {
		return rand.Intn(len(weights))
	}

	r := rand.Float64() * totalWeight
	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if r <= cumulative {
			return i
		}
	}

	return len(weights) - 1
}

// Content Challenge Index Helpers

// AddContentChallengeToStatusIndex adds a content challenge to the status index
func (k Keeper) AddContentChallengeToStatusIndex(ctx context.Context, cc types.ContentChallenge) error {
	return k.ContentChallengesByStatus.Set(ctx, collections.Join(int32(cc.Status), cc.Id))
}

// RemoveContentChallengeFromStatusIndex removes a content challenge from the status index
func (k Keeper) RemoveContentChallengeFromStatusIndex(ctx context.Context, status types.ContentChallengeStatus, id uint64) error {
	return k.ContentChallengesByStatus.Remove(ctx, collections.Join(int32(status), id))
}

// UpdateContentChallengeStatusIndex updates the status index when content challenge status changes
func (k Keeper) UpdateContentChallengeStatusIndex(ctx context.Context, oldStatus, newStatus types.ContentChallengeStatus, id uint64) error {
	if oldStatus == newStatus {
		return nil
	}
	if err := k.RemoveContentChallengeFromStatusIndex(ctx, oldStatus, id); err != nil {
		if !isNotFoundError(err) {
			return err
		}
	}
	return k.ContentChallengesByStatus.Set(ctx, collections.Join(int32(newStatus), id))
}

// IterateContentChallengesByStatus iterates over content challenges with a specific status
func (k Keeper) IterateContentChallengesByStatus(ctx context.Context, status types.ContentChallengeStatus, fn func(id uint64, cc types.ContentChallenge) bool) error {
	rng := collections.NewPrefixedPairRange[int32, uint64](int32(status))
	return k.ContentChallengesByStatus.Walk(ctx, rng, func(key collections.Pair[int32, uint64]) (stop bool, err error) {
		cc, err := k.ContentChallenge.Get(ctx, key.K2())
		if err != nil {
			return false, nil // Skip if not found
		}
		return fn(key.K2(), cc), nil
	})
}

// IterateActiveContentChallenges iterates over active content challenges.
func (k Keeper) IterateActiveContentChallenges(ctx context.Context, fn func(index int64, cc types.ContentChallenge) bool) {
	_ = k.IterateContentChallengesByStatus(ctx, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE, func(id uint64, cc types.ContentChallenge) bool {
		return fn(int64(id), cc)
	})
}
