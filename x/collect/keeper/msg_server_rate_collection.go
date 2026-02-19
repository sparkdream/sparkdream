package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) RateCollection(ctx context.Context, msg *types.MsgRateCollection) (*types.MsgRateCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Creator must be registered active curator with bond >= min_curator_bond
	curator, err := k.Curator.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNotCurator, msg.Creator)
	}
	if !curator.Active {
		return nil, errorsmod.Wrap(types.ErrNotCurator, "curator is not active")
	}
	if curator.BondAmount.LT(params.MinCuratorBond) {
		return nil, errorsmod.Wrapf(types.ErrCuratorBondInsufficient, "bond %s < min %s", curator.BondAmount, params.MinCuratorBond)
	}

	// Creator must meet min_curator_trust_level (re-check on every rating)
	if !k.meetsMinTrustLevel(ctx, msg.Creator, params.MinCuratorTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrTrustLevelTooLow, "must be at or above %s", params.MinCuratorTrustLevel)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Curator registered for at least min_curator_age_blocks
	if currentBlock-curator.RegisteredAt < params.MinCuratorAgeBlocks {
		return nil, errorsmod.Wrapf(types.ErrCuratorTooNew, "registered %d blocks ago, need %d",
			currentBlock-curator.RegisteredAt, params.MinCuratorAgeBlocks)
	}

	// Collection must be VISIBILITY_PUBLIC, status=ACTIVE, community_feedback_enabled=true
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, "collection not found")
	}
	if coll.Visibility != types.Visibility_VISIBILITY_PUBLIC {
		return nil, errorsmod.Wrap(types.ErrCannotRatePrivate, "collection is not public")
	}
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
		return nil, errorsmod.Wrap(types.ErrNotPublicActive, "collection is not active")
	}
	if !coll.CommunityFeedbackEnabled {
		return nil, errorsmod.Wrap(types.ErrNotPublicActive, "community feedback is disabled")
	}

	// Collection not expired
	if coll.ExpiresAt > 0 && coll.ExpiresAt <= currentBlock {
		return nil, errorsmod.Wrap(types.ErrCollectionExpired, "collection has expired")
	}

	// Creator must not be owner or collaborator
	if coll.Owner == msg.Creator {
		return nil, errorsmod.Wrap(types.ErrCannotRateOwnCollection, "curator is collection owner")
	}
	isCollab, _, err := k.IsCollaborator(ctx, coll.Id, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check collaborator status")
	}
	if isCollab {
		return nil, errorsmod.Wrap(types.ErrCannotRateOwnCollection, "curator is a collaborator")
	}

	// Creator must not have active (non-overturned) review for this collection
	var activeReviewCount uint32
	hasExistingReview := false
	err = k.CurationReviewsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](msg.CollectionId),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			review, err := k.CurationReview.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			if !review.Overturned {
				activeReviewCount++
				if review.Curator == msg.Creator {
					hasExistingReview = true
					return true, nil // stop walking
				}
			}
			return false, nil
		},
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to walk reviews")
	}
	if hasExistingReview {
		return nil, errorsmod.Wrap(types.ErrAlreadyReviewed, msg.Creator)
	}

	// Active reviews < max_reviews_per_collection
	if activeReviewCount >= params.MaxReviewsPerCollection {
		return nil, errorsmod.Wrapf(types.ErrMaxReviews, "%d reviews", activeReviewCount)
	}

	// tags count <= max_tags_per_review, each tag <= max_tag_length
	if uint32(len(msg.Tags)) > params.MaxTagsPerReview {
		return nil, errorsmod.Wrapf(types.ErrMaxTags, "%d tags, max %d", len(msg.Tags), params.MaxTagsPerReview)
	}
	for _, tag := range msg.Tags {
		if uint32(len(tag)) > params.MaxTagLength {
			return nil, errorsmod.Wrapf(types.ErrTagTooLong, "tag %q exceeds max length %d", tag, params.MaxTagLength)
		}
	}

	// comment <= max_review_comment_length
	if uint32(len(msg.Comment)) > params.MaxReviewCommentLength {
		return nil, errorsmod.Wrapf(types.ErrReasonTooLong, "comment length %d exceeds max %d", len(msg.Comment), params.MaxReviewCommentLength)
	}

	// verdict must be UP or DOWN
	if msg.Verdict != types.CurationVerdict_CURATION_VERDICT_UP && msg.Verdict != types.CurationVerdict_CURATION_VERDICT_DOWN {
		return nil, errorsmod.Wrap(types.ErrInvalidFlagReason, "verdict must be UP or DOWN")
	}

	// Create CurationReview
	reviewID, err := k.CurationReviewSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get next review ID")
	}

	review := types.CurationReview{
		Id:           reviewID,
		CollectionId: msg.CollectionId,
		Curator:      msg.Creator,
		Verdict:      msg.Verdict,
		Tags:         msg.Tags,
		Comment:      msg.Comment,
		CreatedAt:    currentBlock,
		Challenged:   false,
		Overturned:   false,
		Challenger:   "",
	}
	if err := k.CurationReview.Set(ctx, reviewID, review); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store review")
	}

	// Increment curator total_reviews
	curator.TotalReviews++
	if err := k.Curator.Set(ctx, msg.Creator, curator); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update curator")
	}

	// Update CurationSummary (up_count/down_count, merge tags, last_reviewed_at)
	summary, err := k.CurationSummary.Get(ctx, msg.CollectionId)
	if err != nil {
		// First review for this collection
		summary = types.CurationSummary{
			CollectionId:   msg.CollectionId,
			UpCount:        0,
			DownCount:      0,
			TopTags:        nil,
			LastReviewedAt: 0,
		}
	}

	if msg.Verdict == types.CurationVerdict_CURATION_VERDICT_UP {
		summary.UpCount++
	} else {
		summary.DownCount++
	}
	summary.LastReviewedAt = currentBlock

	// Merge tags into summary
	for _, tag := range msg.Tags {
		found := false
		for i, tc := range summary.TopTags {
			if tc.Tag == tag {
				summary.TopTags[i].Count++
				found = true
				break
			}
		}
		if !found {
			summary.TopTags = append(summary.TopTags, types.TagCount{Tag: tag, Count: 1})
		}
	}

	if err := k.CurationSummary.Set(ctx, msg.CollectionId, summary); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update curation summary")
	}

	// Set indexes: CurationReviewsByCollection, CurationReviewsByCurator
	if err := k.CurationReviewsByCollection.Set(ctx, collections.Join(msg.CollectionId, reviewID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set review-by-collection index")
	}
	if err := k.CurationReviewsByCurator.Set(ctx, collections.Join(msg.Creator, reviewID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set review-by-curator index")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_rated",
		sdk.NewAttribute("review_id", strconv.FormatUint(reviewID, 10)),
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("curator", msg.Creator),
		sdk.NewAttribute("verdict", msg.Verdict.String()),
	))

	return &types.MsgRateCollectionResponse{ReviewId: reviewID}, nil
}
