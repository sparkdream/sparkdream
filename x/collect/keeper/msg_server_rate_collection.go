package keeper

import (
	"context"
	"errors"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) RateCollection(ctx context.Context, msg *types.MsgRateCollection) (*types.MsgRateCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Creator must hold an active bonded role (ROLE_TYPE_COLLECT_CURATOR) in
	// x/rep. Registration is via MsgBondRole there — not via this module.
	if k.late.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrNotCurator, "rep keeper not wired")
	}
	// NORMAL and RECOVERY can both rate; DEMOTED cannot. UNBONDING can rate
	// only while the staying bond (current - pending_unbond) still covers
	// min_bond, so a partial unbond keeps the curator working while a full
	// exit deauthorizes it. Liability containment comes from rep's
	// pending-aware ReserveBond, not from the status flag: a rating reserved
	// mid-unbond draws only on staying bond. Same gate as MsgHideContent.
	role, err := k.late.repKeeper.EligibleForRole(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrNotCurator, "%s: curator status %s: %v", msg.Creator, role.BondStatus.String(), err)
	}

	// Creator must meet min_curator_trust_level (re-check on every rating).
	if !k.meetsMinTrustLevel(ctx, msg.Creator, params.MinCuratorTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrTrustLevelTooLow, "must be at or above %s", params.MinCuratorTrustLevel)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Curator daily review rate limit (cap at max_reviews_per_collection per day)
	if err := k.checkDailyLimit(ctx, msg.Creator, currentBlock, "review", params.MaxReviewsPerCollection); err != nil {
		return nil, errorsmod.Wrap(err, "curator daily review limit exceeded")
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
	if k.sameMember(ctx, coll.Owner, msg.Creator) {
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
				if k.sameMember(ctx, review.Curator, msg.Creator) {
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

	// Validate review tags against the shared x/rep tag registry and bump
	// usage metadata for each accepted tag.
	if err := k.validateTags(ctx, msg.Tags, params.MaxTagsPerReview, params.MaxTagLength, sdkCtx.BlockTime().Unix()); err != nil {
		return nil, err
	}

	// comment <= max_review_comment_length
	if commontypes.Saturate[uint32](len(msg.Comment)) > params.MaxReviewCommentLength {
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
		Id:             reviewID,
		CollectionId:   msg.CollectionId,
		Curator:        msg.Creator,
		Verdict:        msg.Verdict,
		Tags:           msg.Tags,
		Comment:        msg.Comment,
		CreatedAt:      currentBlock,
		Challenged:     false,
		Overturned:     false,
		Challenger:     "",
		CommittedSlash: math.ZeroInt(),
	}
	if err := k.CurationReview.Set(ctx, reviewID, review); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store review")
	}

	// Bump per-module curator activity counters (collect-specific).
	activity, err := k.CuratorActivity.Get(ctx, msg.Creator)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	if activity.Address == "" {
		activity.Address = msg.Creator
	}
	activity.TotalReviews++
	if err := k.CuratorActivity.Set(ctx, msg.Creator, activity); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update curator activity")
	}
	// Stamp BondedRole activity timestamp on rep's side (tracks inactivity).
	if err := k.late.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, msg.Creator); err != nil {
		return nil, err
	}
	// Record the rating on the shared RoleActivity counters too. The SPARK pool
	// gates on challenge outcomes rather than raw volume, so this does not by
	// itself earn anything — it keeps the shared record a complete picture of
	// the role holder's work, the way forum reports hides.
	if err := k.late.repKeeper.RecordRoleAction(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
		msg.Creator, reptypes.ActionKindCollectCuration); err != nil {
		return nil, err
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
