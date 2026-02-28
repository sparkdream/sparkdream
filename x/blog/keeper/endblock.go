package keeper

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/blog/types"

	reptypes "sparkdream/x/rep/types"
)

// EndBlock runs at the end of each block. Handles TTL expiry, subsidy draw, and nullifier pruning.
func (k Keeper) EndBlock(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// Phase 1: TTL Expiry
	k.processExpiredContent(ctx, blockTime)

	// Phase 2: Subsidy Draw
	k.processSubsidyDraw(ctx, blockTime)

	// Phase 3: Nullifier Pruning
	k.pruneNullifiers(ctx, blockTime)

	return nil
}

// getEpochDuration returns the epoch duration from the season keeper, or the default.
func (k Keeper) getEpochDuration(ctx context.Context) int64 {
	if k.seasonKeeper != nil {
		return k.seasonKeeper.GetEpochDuration(ctx)
	}
	return DefaultEpochDuration
}

// processExpiredContent iterates the expiry index and tombstones or upgrades expired content.
func (k Keeper) processExpiredContent(ctx context.Context, blockTime int64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ExpiryKey))

	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()

	// Build an end key: blockTime+1 encoded as big-endian to get all entries <= blockTime
	endBz := make([]byte, 8)
	binary.BigEndian.PutUint64(endBz, uint64(blockTime+1))

	iter := store.Iterator(nil, endBz)
	defer iter.Close()

	// Collect keys to delete after iteration to avoid modifying store during iteration
	type expiryEntry struct {
		fullKey     []byte
		contentType string
		id          uint64
		expiresAt   int64
	}
	var entries []expiryEntry

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		// Key format from expiryKey(): {timestamp_8bytes}/{type}/{id_8bytes}
		// where type separator is "/{type}/"
		if len(key) < 18 { // 8 (ts) + min 2 (separator) + 8 (id) = 18 minimum
			continue
		}
		expiresAt := int64(binary.BigEndian.Uint64(key[:8]))
		// Parse "/{type}/" from key[8:]
		rest := string(key[8:])
		// rest should be "/{type}/" + 8 bytes of id
		// The id is the last 8 bytes of the full key
		idBytes := key[len(key)-8:]
		id := binary.BigEndian.Uint64(idBytes)
		// contentType is between the separators
		middle := rest[:len(rest)-8] // remove id bytes
		contentType := strings.Trim(middle, "/")

		entries = append(entries, expiryEntry{
			fullKey:     append([]byte{}, key...),
			contentType: contentType,
			id:          id,
			expiresAt:   expiresAt,
		})
	}

	for _, entry := range entries {
		switch entry.contentType {
		case "post":
			k.processExpiredPost(ctx, sdkCtx, entry.id, entry.expiresAt, moduleAddr)
		case "reply":
			k.processExpiredReply(ctx, sdkCtx, entry.id, entry.expiresAt, moduleAddr)
		default:
			// Unknown content type, just remove from index
			store.Delete(entry.fullKey)
		}
	}
}

// processExpiredPost handles a single expired post: upgrade to permanent if creator is now active, else tombstone.
func (k Keeper) processExpiredPost(ctx context.Context, sdkCtx sdk.Context, id uint64, expiresAt int64, moduleAddr string) {
	post, found := k.GetPost(ctx, id)
	if !found {
		// Post gone, just clean up expiry index
		k.RemoveFromExpiryIndex(ctx, expiresAt, "post", id)
		return
	}

	if post.Status == types.PostStatus_POST_STATUS_DELETED {
		// Already deleted, just clean up
		k.RemoveFromExpiryIndex(ctx, expiresAt, "post", id)
		return
	}

	// Check if creator is now an active member (upgrade path — non-anonymous only)
	if post.Creator != moduleAddr {
		creatorAccAddr, err := sdk.AccAddressFromBech32(post.Creator)
		if err == nil && k.isActiveMember(ctx, creatorAccAddr) {
			// Upgrade to permanent
			post.ExpiresAt = 0
			k.SetPost(ctx, post)
			k.RemoveFromExpiryIndex(ctx, expiresAt, "post", id)
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"blog.post.upgraded",
				sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
				sdk.NewAttribute("creator", post.Creator),
			))
			return
		}
	}

	// Conviction check (anonymous only): if creator is module account and conviction renewal is enabled
	if post.Creator == moduleAddr && k.repKeeper != nil {
		params, err := k.Params.Get(ctx)
		if err == nil && params.ConvictionRenewalThreshold.IsPositive() {
			conviction, err := k.repKeeper.GetContentConviction(ctx, reptypes.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, id)
			if err == nil && conviction.GTE(params.ConvictionRenewalThreshold) {
				blockTime := sdkCtx.BlockTime().Unix()
				newExpiresAt := blockTime + params.ConvictionRenewalPeriod
				k.RemoveFromExpiryIndex(ctx, expiresAt, "post", id)

				if !post.ConvictionSustained {
					// Entering conviction-sustained state (first expiry)
					post.ConvictionSustained = true
					post.ExpiresAt = newExpiresAt
					k.SetPost(ctx, post)
					k.AddToExpiryIndex(ctx, newExpiresAt, "post", id)
					sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
						"blog.post.conviction_sustained",
						sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
						sdk.NewAttribute("conviction_score", conviction.String()),
						sdk.NewAttribute("new_expires_at", fmt.Sprintf("%d", newExpiresAt)),
					))
				} else {
					// Renewal (already conviction-sustained)
					post.ExpiresAt = newExpiresAt
					k.SetPost(ctx, post)
					k.AddToExpiryIndex(ctx, newExpiresAt, "post", id)
					sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
						"blog.post.renewed",
						sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
						sdk.NewAttribute("conviction_score", conviction.String()),
						sdk.NewAttribute("new_expires_at", fmt.Sprintf("%d", newExpiresAt)),
					))
				}
				return
			}
			// Conviction dropped below threshold — clear conviction_sustained, proceed to tombstone
			if post.ConvictionSustained {
				post.ConvictionSustained = false
			}
		}
	}

	// Tombstone: clear content, mark deleted
	post.Title = ""
	post.Body = ""
	post.Status = types.PostStatus_POST_STATUS_DELETED
	k.SetPost(ctx, post)
	k.RemoveFromExpiryIndex(ctx, expiresAt, "post", id)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"blog.post.expired",
		sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
	))
}

// processExpiredReply handles a single expired reply: upgrade to permanent if creator is now active, else tombstone.
func (k Keeper) processExpiredReply(ctx context.Context, sdkCtx sdk.Context, id uint64, expiresAt int64, moduleAddr string) {
	reply, found := k.GetReply(ctx, id)
	if !found {
		k.RemoveFromExpiryIndex(ctx, expiresAt, "reply", id)
		return
	}

	if reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
		k.RemoveFromExpiryIndex(ctx, expiresAt, "reply", id)
		return
	}

	// Check if creator is now an active member (upgrade path — non-anonymous only)
	if reply.Creator != moduleAddr {
		creatorAccAddr, err := sdk.AccAddressFromBech32(reply.Creator)
		if err == nil && k.isActiveMember(ctx, creatorAccAddr) {
			reply.ExpiresAt = 0
			k.SetReply(ctx, reply)
			k.RemoveFromExpiryIndex(ctx, expiresAt, "reply", id)
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"blog.reply.upgraded",
				sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
				sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
				sdk.NewAttribute("creator", reply.Creator),
			))
			return
		}
	}

	// Conviction check (anonymous only): if creator is module account and conviction renewal is enabled
	if reply.Creator == moduleAddr && k.repKeeper != nil {
		params, err := k.Params.Get(ctx)
		if err == nil && params.ConvictionRenewalThreshold.IsPositive() {
			conviction, err := k.repKeeper.GetContentConviction(ctx, reptypes.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, id)
			if err == nil && conviction.GTE(params.ConvictionRenewalThreshold) {
				blockTime := sdkCtx.BlockTime().Unix()
				newExpiresAt := blockTime + params.ConvictionRenewalPeriod
				k.RemoveFromExpiryIndex(ctx, expiresAt, "reply", id)

				if !reply.ConvictionSustained {
					// Entering conviction-sustained state (first expiry)
					reply.ConvictionSustained = true
					reply.ExpiresAt = newExpiresAt
					k.SetReply(ctx, reply)
					k.AddToExpiryIndex(ctx, newExpiresAt, "reply", id)
					sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
						"blog.reply.conviction_sustained",
						sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
						sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
						sdk.NewAttribute("conviction_score", conviction.String()),
						sdk.NewAttribute("new_expires_at", fmt.Sprintf("%d", newExpiresAt)),
					))
				} else {
					// Renewal (already conviction-sustained)
					reply.ExpiresAt = newExpiresAt
					k.SetReply(ctx, reply)
					k.AddToExpiryIndex(ctx, newExpiresAt, "reply", id)
					sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
						"blog.reply.renewed",
						sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
						sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
						sdk.NewAttribute("conviction_score", conviction.String()),
						sdk.NewAttribute("new_expires_at", fmt.Sprintf("%d", newExpiresAt)),
					))
				}
				return
			}
			// Conviction dropped below threshold — clear conviction_sustained, proceed to tombstone
			if reply.ConvictionSustained {
				reply.ConvictionSustained = false
			}
		}
	}

	// Tombstone: clear content, mark deleted
	wasActive := reply.Status == types.ReplyStatus_REPLY_STATUS_ACTIVE
	reply.Body = ""
	reply.Status = types.ReplyStatus_REPLY_STATUS_DELETED
	k.SetReply(ctx, reply)
	k.RemoveFromExpiryIndex(ctx, expiresAt, "reply", id)

	// Decrement parent post reply count if reply was active
	if wasActive {
		post, found := k.GetPost(ctx, reply.PostId)
		if found && post.ReplyCount > 0 {
			post.ReplyCount--
			k.SetPost(ctx, post)
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"blog.reply.expired",
		sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
	))
}

// processSubsidyDraw draws anonymous posting subsidy from the commons treasury each epoch.
func (k Keeper) processSubsidyDraw(ctx context.Context, blockTime int64) {
	if k.commonsKeeper == nil {
		return
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}

	if params.AnonSubsidyBudgetPerEpoch.Amount.IsNil() || !params.AnonSubsidyBudgetPerEpoch.IsPositive() {
		return
	}

	epochDuration := k.getEpochDuration(ctx)
	if epochDuration <= 0 {
		return
	}

	epoch := uint64(blockTime) / uint64(epochDuration)
	lastEpoch := k.GetAnonSubsidyLastEpoch(ctx)

	if epoch > lastEpoch {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		moduleAddr := authtypes.NewModuleAddress(types.ModuleName)

		err := k.commonsKeeper.SpendFromTreasury(
			ctx,
			"commons",
			moduleAddr,
			sdk.NewCoins(params.AnonSubsidyBudgetPerEpoch),
		)
		if err != nil {
			// Don't fail the block; treasury may be empty
			sdkCtx.Logger().Info("blog: failed to draw anon subsidy", "error", err)
			return
		}

		k.SetAnonSubsidyLastEpoch(ctx, epoch)

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"blog.subsidy.drawn",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epoch)),
			sdk.NewAttribute("amount", params.AnonSubsidyBudgetPerEpoch.String()),
			sdk.NewAttribute("source_treasury", "commons"),
		))
	}
}

// pruneNullifiers removes stale post nullifiers from past epochs.
// Only prunes domain=1 (post nullifiers) since they're epoch-scoped.
func (k Keeper) pruneNullifiers(ctx context.Context, blockTime int64) {
	epochDuration := k.getEpochDuration(ctx)
	if epochDuration <= 0 {
		return
	}

	currentEpoch := uint64(blockTime) / uint64(epochDuration)
	if currentEpoch <= 1 {
		return
	}

	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonNullifierKey))

	// Build prefix for domain=1 nullifiers
	domainPrefix := GetPostIDBytes(1) // domain=1

	iter := store.Iterator(domainPrefix, nil)
	defer iter.Close()

	var keysToDelete [][]byte
	pruneCount := 0

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		// Key format: domain(8) + scope(8) + nullifier_hex
		if len(key) < 17 {
			continue
		}

		// Verify we're still in domain=1
		domain := binary.BigEndian.Uint64(key[:8])
		if domain != 1 {
			break // Past domain=1 entries
		}

		scope := binary.BigEndian.Uint64(key[8:16])
		// Prune if scope is from more than 1 epoch ago
		if scope < currentEpoch-1 {
			keysToDelete = append(keysToDelete, append([]byte{}, key...))
			pruneCount++
		}
	}

	for _, key := range keysToDelete {
		store.Delete(key)
	}

	if pruneCount > 0 {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"blog.nullifiers.pruned",
			sdk.NewAttribute("domain", "1"),
			sdk.NewAttribute("pruned_before_scope", fmt.Sprintf("%d", currentEpoch-1)),
			sdk.NewAttribute("count", fmt.Sprintf("%d", pruneCount)),
		))
	}
}
