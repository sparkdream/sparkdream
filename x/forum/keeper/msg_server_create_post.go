package keeper

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// tagPattern validates tag format: alphanumeric and hyphens only
var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// validatePostTags validates a list of tags for use on a post and updates tag metadata.
func (k msgServer) validatePostTags(ctx context.Context, tags []string, now int64) error {
	if uint64(len(tags)) > types.DefaultMaxTagsPerPost {
		return errorsmod.Wrapf(types.ErrTagLimitExceeded, "max %d tags per post", types.DefaultMaxTagsPerPost)
	}

	seen := make(map[string]bool, len(tags))
	for _, tagName := range tags {
		// Check for duplicates
		if seen[tagName] {
			return errorsmod.Wrapf(types.ErrInvalidTag, "duplicate tag: %s", tagName)
		}
		seen[tagName] = true

		// Check length
		if uint64(len(tagName)) > types.DefaultMaxTagLength {
			return errorsmod.Wrapf(types.ErrMaxTagLength, "tag %q exceeds max length %d", tagName, types.DefaultMaxTagLength)
		}

		// Check format
		if !tagPattern.MatchString(tagName) {
			return errorsmod.Wrapf(types.ErrInvalidTag, "tag %q does not match required format", tagName)
		}

		// Check tag exists
		tag, err := k.Tag.Get(ctx, tagName)
		if err != nil {
			return errorsmod.Wrapf(types.ErrTagNotFound, "tag %q not found", tagName)
		}

		// Check tag is not reserved
		_, err = k.ReservedTag.Get(ctx, tagName)
		if err == nil {
			return errorsmod.Wrapf(types.ErrReservedTag, "tag %q is reserved", tagName)
		}

		// Update tag usage metadata
		tag.UsageCount++
		tag.LastUsedAt = now
		if err := k.Tag.Set(ctx, tagName, tag); err != nil {
			return errorsmod.Wrap(err, "failed to update tag metadata")
		}
	}

	return nil
}

func (k msgServer) CreatePost(ctx context.Context, msg *types.MsgCreatePost) (*types.MsgCreatePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check forum_paused param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ForumPaused {
		return nil, types.ErrForumPaused
	}

	// Validate category exists
	category, err := k.Category.Get(ctx, msg.CategoryId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCategoryNotFound, fmt.Sprintf("category %d not found", msg.CategoryId))
	}

	// Check category write permissions
	isMember := k.IsMember(ctx, msg.Creator)
	isAuthorized := k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	if category.AdminOnlyWrite && !isAuthorized {
		return nil, types.ErrAdminOnlyWrite
	}
	if category.MembersOnlyWrite && !isMember && !isAuthorized {
		return nil, types.ErrMembersOnlyWrite
	}

	// Variables for thread context
	var rootID uint64
	var parentPost types.Post
	var depth uint64

	// Handle replies
	if msg.ParentId != 0 {
		parentPost, err = k.Post.Get(ctx, msg.ParentId)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrParentPostNotFound, fmt.Sprintf("parent post %d not found", msg.ParentId))
		}

		// Determine root ID
		if parentPost.ParentId == 0 {
			rootID = msg.ParentId
		} else {
			rootID = parentPost.RootId
		}

		// Load root post to check thread lock
		rootPost, err := k.Post.Get(ctx, rootID)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("root post %d not found", rootID))
		}

		// Check if thread is locked
		if rootPost.Locked {
			return nil, types.ErrThreadLocked
		}

		// Check reply depth
		depth = parentPost.Depth + 1
		if depth > uint64(types.DefaultMaxReplyDepth) {
			return nil, errorsmod.Wrapf(types.ErrMaxReplyDepthExceeded, "max depth is %d", types.DefaultMaxReplyDepth)
		}
	}

	// Validate content
	if msg.Content == "" {
		return nil, types.ErrEmptyContent
	}
	if uint64(len(msg.Content)) > types.DefaultMaxContentSize {
		return nil, errorsmod.Wrapf(types.ErrContentTooLarge, "max size is %d bytes", types.DefaultMaxContentSize)
	}

	// Charge cost_per_byte storage fee (applies to all posters, burned)
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		contentBytes := int64(len(msg.Content))
		storageFee := sdk.NewCoin(params.CostPerByte.Denom,
			params.CostPerByte.Amount.MulRaw(contentBytes))
		if storageFee.IsPositive() {
			creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(storageFee)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to charge storage fee")
			}
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(storageFee)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to burn storage fee")
			}
		}
	}

	// Validate tags
	if len(msg.Tags) > 0 {
		if err := k.validatePostTags(ctx, msg.Tags, now); err != nil {
			return nil, err
		}
	}

	// Check rate limit
	if err := k.checkAndUpdateRateLimit(ctx, msg.Creator, now); err != nil {
		return nil, err
	}

	// Generate post ID
	postID, err := k.PostSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate post ID")
	}

	// Determine expiration (member = permanent, non-member = ephemeral)
	var expirationTime int64
	if !isMember {
		expirationTime = now + types.DefaultEphemeralTTL
		// Charge spam tax to non-members
		if params.SpamTax.IsPositive() {
			creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(params.SpamTax)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to charge spam tax")
			}
		}
	}

	// Create post
	post := types.Post{
		PostId:         postID,
		CategoryId:     msg.CategoryId,
		RootId:         rootID,
		ParentId:       msg.ParentId,
		Author:         msg.Creator,
		Content:        msg.Content,
		CreatedAt:      now,
		ExpirationTime: expirationTime,
		Status:         types.PostStatus_POST_STATUS_ACTIVE,
		Depth:          depth,
		Tags:           msg.Tags,
		ContentType:    msg.ContentType,
	}

	// Store post
	if err := k.Post.Set(ctx, postID, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store post")
	}

	// Handle salvation for ephemeral parent posts
	if msg.ParentId != 0 && isMember && parentPost.ExpirationTime > 0 {
		// Member replying to ephemeral post - attempt salvation
		if err := k.salvageAncestors(ctx, msg.Creator, msg.ParentId, now); err != nil {
			// Log but don't fail - salvation is best effort
			sdkCtx.Logger().Info("salvation failed", "error", err)
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_created",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
			sdk.NewAttribute("category_id", fmt.Sprintf("%d", msg.CategoryId)),
			sdk.NewAttribute("parent_id", fmt.Sprintf("%d", msg.ParentId)),
			sdk.NewAttribute("author", msg.Creator),
			sdk.NewAttribute("is_ephemeral", fmt.Sprintf("%t", expirationTime > 0)),
			sdk.NewAttribute("tags", strings.Join(msg.Tags, ",")),
		),
	)

	return &types.MsgCreatePostResponse{}, nil
}

// checkAndUpdateRateLimit checks and updates the rate limit for a user.
func (k msgServer) checkAndUpdateRateLimit(ctx context.Context, addr string, now int64) error {
	rateLimit, err := k.UserRateLimit.Get(ctx, addr)
	if err != nil {
		// Create new rate limit record
		rateLimit = types.UserRateLimit{
			UserAddress:        addr,
			CurrentEpochCount:  0,
			PreviousEpochCount: 0,
			CurrentEpochStart:  now,
			LastPostTime:       0,
		}
	}

	// Epoch rotation (24h epochs)
	const epochDuration int64 = 86400
	if now-rateLimit.CurrentEpochStart >= epochDuration {
		rateLimit.PreviousEpochCount = rateLimit.CurrentEpochCount
		rateLimit.CurrentEpochCount = 0
		rateLimit.CurrentEpochStart = now
	}

	// Calculate effective count using sliding window approximation
	var overlapRatio float64
	elapsed := now - rateLimit.CurrentEpochStart
	if elapsed < epochDuration {
		overlapRatio = float64(epochDuration-elapsed) / float64(epochDuration)
	}
	effectiveCount := float64(rateLimit.CurrentEpochCount) + float64(rateLimit.PreviousEpochCount)*overlapRatio

	if effectiveCount >= float64(types.DefaultDailyPostLimit) {
		return types.ErrRateLimitExceeded
	}

	// Update rate limit
	rateLimit.CurrentEpochCount++
	rateLimit.LastPostTime = now

	if err := k.UserRateLimit.Set(ctx, addr, rateLimit); err != nil {
		return errorsmod.Wrap(err, "failed to update rate limit")
	}

	return nil
}

// salvageAncestors salvages ephemeral ancestor posts when a member replies.
func (k msgServer) salvageAncestors(ctx context.Context, memberAddr string, parentID uint64, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check if member is eligible to salvage (membership duration)
	memberSince := k.GetMemberSince(ctx, memberAddr)
	if now-memberSince < types.DefaultMinMembershipForSalvation {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"salvation_denied",
				sdk.NewAttribute("reason", "membership_too_new"),
				sdk.NewAttribute("member", memberAddr),
			),
		)
		return nil // Not an error, just skip salvation
	}

	// Check salvation rate limit
	salvationStatus, err := k.MemberSalvationStatus.Get(ctx, memberAddr)
	if err != nil {
		salvationStatus = types.MemberSalvationStatus{
			Address:         memberAddr,
			MemberSince:     memberSince,
			CanSalvage:      true,
			EpochSalvations: 0,
			EpochStart:      now,
		}
	}

	// Reset epoch if needed
	const salvationEpochDuration int64 = 86400 // 24h
	if now-salvationStatus.EpochStart >= salvationEpochDuration {
		salvationStatus.EpochSalvations = 0
		salvationStatus.EpochStart = now
	}

	if salvationStatus.EpochSalvations >= types.DefaultMaxSalvationsPerDay {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"salvation_denied",
				sdk.NewAttribute("reason", "rate_limit_exceeded"),
				sdk.NewAttribute("member", memberAddr),
			),
		)
		return nil
	}

	// Calculate effective depth limit based on remaining budget
	remainingSalvations := types.DefaultMaxSalvationsPerDay - salvationStatus.EpochSalvations
	effectiveDepth := types.DefaultMaxSalvationDepth
	if remainingSalvations < effectiveDepth {
		effectiveDepth = remainingSalvations
	}

	// Salvage ancestors recursively
	currentID := parentID
	depth := uint64(0)
	salvagedCount := uint64(0)

	for currentID != 0 && depth < effectiveDepth {
		post, err := k.Post.Get(ctx, currentID)
		if err != nil {
			break
		}

		// Only salvage ephemeral posts
		if post.ExpirationTime > 0 {
			// Check if thread is locked
			var rootID uint64
			if post.ParentId == 0 {
				rootID = post.PostId
			} else {
				rootID = post.RootId
			}

			rootPost, err := k.Post.Get(ctx, rootID)
			if err == nil && rootPost.Locked {
				sdkCtx.EventManager().EmitEvent(
					sdk.NewEvent(
						"salvation_denied",
						sdk.NewAttribute("post_id", fmt.Sprintf("%d", post.PostId)),
						sdk.NewAttribute("reason", "thread_locked"),
					),
				)
				break
			}

			// Salvage: make permanent
			post.ExpirationTime = 0
			if err := k.Post.Set(ctx, post.PostId, post); err != nil {
				return errorsmod.Wrap(err, "failed to salvage post")
			}
			salvagedCount++
		}

		currentID = post.ParentId
		depth++
	}

	// Update salvation status
	salvationStatus.EpochSalvations += salvagedCount
	if err := k.MemberSalvationStatus.Set(ctx, memberAddr, salvationStatus); err != nil {
		return errorsmod.Wrap(err, "failed to update salvation status")
	}

	if salvagedCount > 0 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"posts_salvaged",
				sdk.NewAttribute("member", memberAddr),
				sdk.NewAttribute("count", fmt.Sprintf("%d", salvagedCount)),
			),
		)
	}

	return nil
}
