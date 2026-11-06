package keeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	commontypes "sparkdream/x/common/types"
	shieldtypes "sparkdream/x/shield/types"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// shieldModuleAddress is the deterministic address for the shield module account.
// When the shield module routes a message via MsgShieldedExec, the ZK proof has
// already verified membership and trust level, so this module bypasses its own checks.
//
// SECURITY NOTE (CROSS-1): This address bypasses the membership check in isMember(),
// and meetsMinTrustLevel() compares it against the level its ZK proof established.
// This is a single point of failure — correctness depends entirely on x/shield's ZK
// proof verification. If the shield module is compromised, membership gating in
// x/collect is bypassed. Per-voter bookkeeping (vote dedup, daily vote limits, the
// own-collection check) does not apply to it since every anonymous member shares it;
// deposits and other validations still apply.
var shieldModuleAddress = authtypes.NewModuleAddress("shield")

const (
	// BlocksPerDay is approximate blocks per day (~6s block time).
	BlocksPerDay int64 = 14400
)

// --- Composite key builders ---

// CollaboratorCompositeKey builds the storage key for a collaborator: "{collectionID}/{address}".
func CollaboratorCompositeKey(collectionID uint64, address string) string {
	return fmt.Sprintf("%d/%s", collectionID, address)
}

// FlagCompositeKey builds the storage key for a flag: "{targetType}/{targetID}".
func FlagCompositeKey(targetType types.FlagTargetType, targetID uint64) string {
	return fmt.Sprintf("%d/%d", int32(targetType), targetID)
}

// ReactionDedupCompositeKey builds the dedup key: "{address}/{targetType}/{targetID}".
func ReactionDedupCompositeKey(address string, targetType types.FlagTargetType, targetID uint64) string {
	return fmt.Sprintf("%s/%d/%d", address, int32(targetType), targetID)
}

// ReactionLimitKey builds the daily rate limit key: "{address}/{day}".
func ReactionLimitCompositeKey(address string, blockHeight int64, category string) string {
	day := blockHeight / BlocksPerDay
	return fmt.Sprintf("%s/%d/%s", address, day, category)
}

// HideRecordTargetKey builds the composite key for hide record by-target index.
func HideRecordTargetCompositeKey(targetType types.FlagTargetType, targetID uint64) string {
	return fmt.Sprintf("%d/%d", int32(targetType), targetID)
}

// --- Member key migration alias helpers ---
//
// Records keep the address they were written with; a member who migrated to
// a fresh key is matched through x/rep's alias contract. The rep keeper is
// late-wired and nil in some unit tests, where plain equality is the answer.

// canActAs reports whether signer may exercise rights recorded against stored.
func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	if k.late.repKeeper == nil {
		return signer == stored
	}
	return k.late.repKeeper.CanActAs(ctx, signer, stored)
}

// sameMember reports whether two stored addresses belong to the same member.
func (k Keeper) sameMember(ctx context.Context, a, b string) bool {
	if k.late.repKeeper == nil {
		return a == b
	}
	return k.late.repKeeper.SameMember(ctx, a, b)
}

// recentAddressesOf is the in-tx alias set (dedup checks).
func (k Keeper) recentAddressesOf(ctx context.Context, member string) []string {
	if k.late.repKeeper == nil {
		return []string{member}
	}
	return k.late.repKeeper.RecentAddressesOf(ctx, member)
}

// allAddressesOf is the full alias set (list queries only).
func (k Keeper) allAddressesOf(ctx context.Context, member string) []string {
	if k.late.repKeeper == nil {
		return []string{member}
	}
	return k.late.repKeeper.AllAddressesOf(ctx, member)
}

// findCollaborator returns the collaborator record on collectionID that
// address may act for: the record is keyed by the address it was added
// with, so each recent alias of address is tried and the hit must pass
// canActAs (a tombstoned signer never matches).
func (k Keeper) findCollaborator(ctx context.Context, collectionID uint64, address string) (types.Collaborator, bool) {
	for _, alias := range k.recentAddressesOf(ctx, address) {
		collab, err := k.Collaborator.Get(ctx, CollaboratorCompositeKey(collectionID, alias))
		if err != nil {
			continue
		}
		if k.canActAs(ctx, address, alias) {
			return collab, true
		}
	}
	return types.Collaborator{}, false
}

// hasReacted reports whether any recent alias of address already reacted to
// the target (ReactionDedup is keyed by the address that reacted).
func (k Keeper) hasReacted(ctx context.Context, address string, targetType types.FlagTargetType, targetID uint64) bool {
	for _, alias := range k.recentAddressesOf(ctx, address) {
		if _, err := k.ReactionDedup.Get(ctx, ReactionDedupCompositeKey(alias, targetType, targetID)); err == nil {
			return true
		}
	}
	return false
}

// --- Access control helpers ---

// HasWriteAccess returns true if the address is the collection owner or an EDITOR/ADMIN collaborator.
func (k Keeper) HasWriteAccess(ctx context.Context, coll types.Collection, address string) (bool, error) {
	if k.canActAs(ctx, address, coll.Owner) {
		return k.isCollectionOwner(ctx, coll, address), nil
	}
	collab, found := k.findCollaborator(ctx, coll.Id, address)
	if !found {
		return false, nil
	}
	if collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR ||
		collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN {
		return true, nil
	}
	return false, nil
}

// IsOwnerOrAdmin returns true if the address is the collection owner or an ADMIN collaborator.
func (k Keeper) IsOwnerOrAdmin(ctx context.Context, coll types.Collection, address string) (bool, error) {
	if k.canActAs(ctx, address, coll.Owner) {
		return k.isCollectionOwner(ctx, coll, address), nil
	}
	collab, found := k.findCollaborator(ctx, coll.Id, address)
	if !found {
		return false, nil
	}
	return collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN, nil
}

// IsCollaborator checks if the address is a collaborator (any role) on the collection.
func (k Keeper) IsCollaborator(ctx context.Context, collectionID uint64, address string) (bool, types.CollaboratorRole, error) {
	collab, found := k.findCollaborator(ctx, collectionID, address)
	if !found {
		return false, types.CollaboratorRole_COLLABORATOR_ROLE_UNSPECIFIED, nil
	}
	return true, collab.Role, nil
}

// deductRepPerTag applies a per-tag reputation deduction on slash paths
// (endorsement burn, collaborator-stake burn, author bond slash). The
// deduction is best-effort — DeductReputation errors when the address is not
// an active member, which is a non-critical condition for a slash
// side-effect (cf. x/reveal's matching pattern). Floored at zero by x/rep.
func (k Keeper) deductRepPerTag(ctx context.Context, addr sdk.AccAddress, tags []string, amount math.LegacyDec) {
	if k.late.repKeeper == nil || amount.IsNil() || !amount.IsPositive() || len(tags) == 0 {
		return
	}
	for _, tag := range tags {
		if err := k.late.repKeeper.DeductReputation(ctx, addr, tag, amount); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deductRepPerTag: repKeeper.DeductReputation failed", "err", err)
		}
	}
}

// restoreAuthorPenalties restores the author bond and per-tag rep penalty
// snapshotted on a HideRecord at hide time. Called from every hide
// reversal that favors the author: sentinel self-correct
// (MsgUnhideContent), appeal overturn (RepAppealTarget.ReverseSentinelAction), and
// appeal timeout (handleAppealedHideExpiry). The unappealed-expiry path
// deletes the content and deliberately does NOT restore.
//
// Collection targets only — items carry neither penalty. Best-effort
// with logged warnings, mirroring the hide-side slash semantics; each
// caller resolves the record exactly once, and RestoreAuthorBond is
// additionally idempotent on the rep side.
func (k Keeper) restoreAuthorPenalties(ctx context.Context, hr types.HideRecord) (bondRestored, repRestored bool) {
	if hr.TargetType != types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION || k.late.repKeeper == nil {
		return false, false
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	coll, err := k.Collection.Get(ctx, hr.TargetId)
	if err != nil {
		return false, false
	}
	ownerBytes, err := k.addressCodec.StringToBytes(coll.Owner)
	if err != nil {
		sdkCtx.Logger().Warn("restore author penalties: bad owner address",
			"collection_id", hr.TargetId, "owner", coll.Owner, "error", err)
		return false, false
	}
	ownerAddr := sdk.AccAddress(ownerBytes)

	if !hr.AuthorBondAmount.IsNil() && hr.AuthorBondAmount.IsPositive() {
		if err := k.late.repKeeper.RestoreAuthorBond(ctx, ownerAddr,
			reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND, hr.TargetId, hr.AuthorBondAmount); err != nil {
			sdkCtx.Logger().Warn("restore author penalties: restore author bond failed",
				"collection_id", hr.TargetId, "error", err)
		} else {
			bondRestored = true
		}
	}
	// Restore the ACTUAL per-tag deductions snapshotted at hide time
	// (min(score-at-hide, penalty) — see MsgHideContent). Restoring the raw
	// penalty param would mint rep from nothing for authors whose deduction
	// was floored at zero.
	for i, tag := range hr.RepPenaltyTags {
		if i >= len(hr.RepPenaltyAmounts) {
			break
		}
		amount, parseErr := math.LegacyNewDecFromStr(hr.RepPenaltyAmounts[i])
		if parseErr != nil || !amount.IsPositive() {
			continue
		}
		if err := k.late.repKeeper.AddReputation(ctx, ownerAddr, tag, amount); err == nil {
			repRestored = true
		}
	}
	return bondRestored, repRestored
}

// releaseOrSlashCollabStake settles a non-member collaborator's locked DREAM
// stake when their record is being removed (via MsgRemoveCollaborator or as
// part of collection deletion). Returns (burned, refunded) DREAM amounts.
//
// Decision rule:
//   - Collaborator carries no stake (member or legacy) → no-op.
//   - Collection status == HIDDEN at settlement time → burn
//     params.NonMemberCollabBurnFraction of the stake; refund the rest.
//   - Otherwise → refund the full stake.
//
// Failures in the rep keeper (e.g. the inviter is no longer a Member because
// they were zeroed) are logged and swallowed so collaborator removal /
// collection deletion is never blocked by stake settlement. Mirrors the
// endorser-stake release pattern in deleteCollectionFull.
func (k Keeper) releaseOrSlashCollabStake(
	ctx context.Context,
	coll types.Collection,
	collab types.Collaborator,
) (burned math.Int, refunded math.Int, err error) {
	burned, refunded = math.ZeroInt(), math.ZeroInt()
	if collab.DreamStake.IsNil() || !collab.DreamStake.IsPositive() {
		return burned, refunded, nil
	}
	if collab.Inviter == "" {
		// Defensive: a non-zero stake without an inviter is malformed state;
		// nothing to refund to.
		return burned, refunded, nil
	}
	inviterAddr, addrErr := k.addressCodec.StringToBytes(collab.Inviter)
	if addrErr != nil {
		return burned, refunded, nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, paramsErr := k.Params.Get(ctx)
	if paramsErr != nil {
		return burned, refunded, paramsErr
	}

	stake := collab.DreamStake
	burnAmt := math.ZeroInt()
	if coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN &&
		!params.NonMemberCollabBurnFraction.IsNil() && params.NonMemberCollabBurnFraction.IsPositive() {
		burnAmt = params.NonMemberCollabBurnFraction.MulInt(stake).TruncateInt()
		if burnAmt.GT(stake) {
			burnAmt = stake
		}
	}

	// Burn the slashed fraction straight out of the lock (one step: a
	// failed burn leaves the lock in place), then release the rest. A failed
	// burn falls back to releasing the whole stake, as before.
	if burnAmt.IsPositive() {
		if burnErr := k.late.repKeeper.BurnLockedDREAM(ctx, inviterAddr, burnAmt); burnErr != nil {
			sdkCtx.Logger().Error("failed to burn inviter DREAM stake fraction",
				"collection_id", coll.Id, "inviter", collab.Inviter, "address", collab.Address, "burn", burnAmt.String(), "error", burnErr)
		} else {
			burned = burnAmt
			// Mirror the DREAM burn with a per-tag rep deduction so the inviter's
			// score on the collection's topic tags reflects the misjudged vouch.
			k.deductRepPerTag(ctx, inviterAddr, coll.Tags, params.CollabInviterRepPenalty)
		}
	}
	rest := stake.Sub(burned)
	if rest.IsPositive() {
		if unlockErr := k.late.repKeeper.UnlockDREAM(ctx, inviterAddr, rest); unlockErr != nil {
			sdkCtx.Logger().Error("failed to unlock inviter DREAM stake",
				"collection_id", coll.Id, "inviter", collab.Inviter, "address", collab.Address, "error", unlockErr)
			return burned, refunded, nil
		}
	}
	refunded = rest
	return burned, refunded, nil
}

// --- Target resolution ---

// GetCollectionOwner returns the owner address of the given collection. Used
// by x/rep to resolve true owners for content self-stake prevention.
func (k Keeper) GetCollectionOwner(ctx context.Context, collectionID uint64) (string, error) {
	coll, err := k.Collection.Get(ctx, collectionID)
	if err != nil {
		return "", fmt.Errorf("collection %d not found: %w", collectionID, err)
	}
	return coll.Owner, nil
}

// GetCollectionForTarget resolves the parent collection for a target (collection or item).
func (k Keeper) GetCollectionForTarget(ctx context.Context, targetType types.FlagTargetType, targetID uint64) (types.Collection, error) {
	switch targetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		return k.Collection.Get(ctx, targetID)
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, err := k.Item.Get(ctx, targetID)
		if err != nil {
			return types.Collection{}, types.ErrItemNotFound
		}
		return k.Collection.Get(ctx, item.CollectionId)
	default:
		return types.Collection{}, fmt.Errorf("invalid target type")
	}
}

// ValidatePublicActiveTarget checks that a target is a public, active collection/item
// with community_feedback_enabled=true.
func (k Keeper) ValidatePublicActiveTarget(ctx context.Context, targetType types.FlagTargetType, targetID uint64) (types.Collection, error) {
	coll, err := k.GetCollectionForTarget(ctx, targetType, targetID)
	if err != nil {
		return types.Collection{}, err
	}
	if coll.Visibility != types.Visibility_VISIBILITY_PUBLIC {
		return types.Collection{}, types.ErrNotPublicActive
	}
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
		return types.Collection{}, types.ErrNotPublicActive
	}

	// For items, also check item status
	if targetType == types.FlagTargetType_FLAG_TARGET_TYPE_ITEM {
		item, err := k.Item.Get(ctx, targetID)
		if err != nil {
			return types.Collection{}, types.ErrItemNotFound
		}
		if item.Status == types.ItemStatus_ITEM_STATUS_HIDDEN {
			return types.Collection{}, types.ErrNotPublicActive
		}
	}

	return coll, nil
}

// ValidatePublicActiveFeedbackTarget is like ValidatePublicActiveTarget but also checks community_feedback_enabled.
func (k Keeper) ValidatePublicActiveFeedbackTarget(ctx context.Context, targetType types.FlagTargetType, targetID uint64) (types.Collection, error) {
	coll, err := k.ValidatePublicActiveTarget(ctx, targetType, targetID)
	if err != nil {
		return types.Collection{}, err
	}
	if !coll.CommunityFeedbackEnabled {
		return types.Collection{}, types.ErrNotPublicActive
	}
	return coll, nil
}

// --- SPARK operations ---

// EscrowSPARK transfers SPARK from an account to the module account (hold).
func (k Keeper) EscrowSPARK(ctx context.Context, from sdk.AccAddress, amount math.Int) error {
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, coins)
}

// RefundSPARK transfers SPARK from the module account to an account (refund).
func (k Keeper) RefundSPARK(ctx context.Context, to sdk.AccAddress, amount math.Int) error {
	if amount.IsZero() {
		return nil
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, to, coins)
}

// BurnSPARK burns SPARK from the module account.
func (k Keeper) BurnSPARK(ctx context.Context, amount math.Int) error {
	if amount.IsZero() {
		return nil
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins)
}

// BurnSPARKFromAccount transfers SPARK to module then burns.
func (k Keeper) BurnSPARKFromAccount(ctx context.Context, from sdk.AccAddress, amount math.Int) error {
	if amount.IsZero() {
		return nil
	}
	if err := k.EscrowSPARK(ctx, from, amount); err != nil {
		return err
	}
	return k.BurnSPARK(ctx, amount)
}

// --- Trust level helpers ---

// TrustLevelIndex returns the numeric index for a trust level (0-4).
func TrustLevelIndex(tl reptypes.TrustLevel) int {
	return int(tl)
}

// ParseTrustLevel parses a trust level string to the enum value.
func ParseTrustLevel(s string) (reptypes.TrustLevel, bool) {
	val, ok := reptypes.TrustLevel_value[s]
	if !ok {
		return 0, false
	}
	return reptypes.TrustLevel(val), true
}

// --- Position management ---

// CompactPositions reassigns sequential positions (0, 1, 2, ...) for all items in a collection.
// WARNING: This is O(n) — it walks all items and updates each item record.
// No longer called automatically on item removal. Positions are allowed to be sparse.
// Can be called explicitly if sequential positions are required (e.g., for reordering).
func (k Keeper) CompactPositions(ctx context.Context, collectionID uint64) error {
	// Collect all item IDs in current position order
	var itemIDs []uint64
	err := k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			itemIDs = append(itemIDs, key.K2())
			return false, nil
		},
	)
	if err != nil {
		return err
	}

	// Clear all position index entries for this collection
	var keysToRemove []collections.Pair[uint64, uint64]
	err = k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			keysToRemove = append(keysToRemove, key)
			return false, nil
		},
	)
	if err != nil {
		return err
	}
	for _, key := range keysToRemove {
		if err := k.ItemsByCollection.Remove(ctx, key); err != nil {
			return err
		}
	}

	// Re-insert with item IDs and update item records with sequential positions
	for i, itemID := range itemIDs {
		newPos := commontypes.Saturate[uint64](i)
		if err := k.ItemsByCollection.Set(ctx, collections.Join(collectionID, itemID)); err != nil {
			return err
		}
		item, err := k.Item.Get(ctx, itemID)
		if err != nil {
			return err
		}
		item.Position = newPos
		if err := k.Item.Set(ctx, itemID, item); err != nil {
			return err
		}
	}

	return nil
}

// InsertAtPosition shifts items at position and above right by one, then sets the new item at position.
func (k Keeper) InsertAtPosition(ctx context.Context, collectionID uint64, position uint64, newItemID uint64) error {
	// ItemsByCollection is a KeySet of (collectionID, itemID), not (collectionID, position).
	// The position is stored on the Item record itself.
	// Walk all items, sort by position, shift those >= target position.
	var items []types.Item
	err := k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			item, err := k.Item.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			items = append(items, item)
			return false, nil
		},
	)
	if err != nil {
		return err
	}

	// Shift items at position and above
	for _, item := range items {
		if item.Position >= position {
			item.Position++
			if err := k.Item.Set(ctx, item.Id, item); err != nil {
				return err
			}
		}
	}

	return nil
}

// --- Cleanup ---

// hasInflightHideAppeal returns true when at least one HideRecord on the
// given collection is currently appealed and unresolved — i.e., the appellant
// filed but the jury has not yet ruled and the appeal deadline has not yet
// expired. Used by deleteCollectionFull to suppress the endorser slash in
// that ambiguous window: a jury that would have upheld the appeal cannot be
// asked retroactively after the collection is gone, so we give the endorser
// the benefit of the doubt rather than over-slash on TTL/owner-delete races.
func (k Keeper) hasInflightHideAppeal(ctx context.Context, collID uint64) bool {
	var inflight bool
	prefix := HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID)
	if err := k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](prefix),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hr, err := k.HideRecord.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			if hr.Appealed && !hr.Resolved {
				inflight = true
				return true, nil // stop walking — one is enough
			}
			return false, nil
		},
	); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("hasInflightHideAppeal: HideRecordByTarget.Walk failed", "err", err)
	}
	return inflight
}

// deleteCollectionAtomic runs deleteCollectionFull in a cache context and
// commits only on success. EndBlocker callers log an error and retry later,
// so a half-applied delete (deposit refunded, then a later step failing)
// must not persist or the next attempt pays the refund again (COL-104).
// MsgDeleteCollection needs no wrapper: a returned error reverts the tx.
func (k Keeper) deleteCollectionAtomic(ctx context.Context, coll types.Collection) error {
	cacheCtx, write := sdk.UnwrapSDKContext(ctx).CacheContext()
	if err := k.deleteCollectionFull(cacheCtx, coll); err != nil {
		return err
	}
	write()
	return nil
}

// deleteCollectionFull removes a collection and all associated state.
// Used by MsgDeleteCollection and EndBlocker TTL pruning.
func (k Keeper) deleteCollectionFull(ctx context.Context, coll types.Collection) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	ownerAddr, err := k.addressCodec.StringToBytes(coll.Owner)
	if err != nil {
		return err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Refund deposits if not burned. Only tracked amounts move: an anonymous
	// collection records zero DepositAmount/ItemDepositTotal, so nothing is
	// paid to the shield account.
	if !coll.DepositBurned {
		totalRefund := coll.DepositAmount.Add(coll.ItemDepositTotal)
		if totalRefund.IsPositive() {
			if err := k.RefundSPARK(ctx, ownerAddr, totalRefund); err != nil {
				return err
			}
		}
	}

	// Settle a still-escrowed endorsement creation fee (refund minus burn
	// fraction). The amount is the one escrowed at creation, never the
	// current param, and it is settled whatever the status is now (a PENDING
	// collection that was hidden still holds its fee).
	// (Anonymous collections never pay the fee and never start PENDING; the
	// chargesDeposits guard keeps the shield account from ever being paid.)
	if fee := escrowedEndorsementFee(coll); fee.IsPositive() {
		if k.chargesDeposits(coll) {
			burnAmt := params.EndorsementDeletionBurnFraction.MulInt(fee).TruncateInt()
			refundAmt := fee.Sub(burnAmt)
			if refundAmt.IsPositive() {
				if err := k.RefundSPARK(ctx, ownerAddr, refundAmt); err != nil {
					return err
				}
			}
			if burnAmt.IsPositive() {
				if err := k.BurnSPARK(ctx, burnAmt); err != nil {
					return err
				}
			}
		}
	}
	// Remove from the pending index by the stored deadline (no-op when the
	// collection never entered, or already left, the endorsement stage).
	k.clearEndorsementPending(ctx, &coll, coll.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING)

	// Handle endorsed collection: release endorser stake if not yet released.
	//
	// If the collection is being deleted while a sentinel hide stands against
	// it (status HIDDEN), the endorser vouched for content that a sentinel
	// later judged hide-worthy. Burn the locked DREAM. Otherwise (TTL on
	// healthy content, appeal restored to ACTIVE, owner delete on ACTIVE,
	// etc.) unlock back to the endorser.
	//
	// Callers must ensure we never reach this point while a hide appeal is
	// in flight (Appealed && !Resolved): the §10.1 TTL pruner skips such
	// collections (defers until the appeal resolves), MsgDeleteCollection
	// rejects HIDDEN status outright with ErrCannotDeleteHidden, and
	// The UPHELD appeal path (RepAppealTarget) pre-persists Resolved=true before calling here. So
	// Status==HIDDEN at this point unambiguously means "sentinel hide stands
	// AND the appeal lane is settled (no appeal, jury rejected, or appeal
	// timed out in sentinel's favor — though the last doesn't currently
	// trigger a delete)."
	if coll.EndorsedBy != "" {
		endorsement, err := k.Endorsement.Get(ctx, coll.Id)
		if err == nil && !endorsement.StakeReleased {
			endorserAddr, addrErr := k.addressCodec.StringToBytes(endorsement.Endorser)
			if addrErr == nil {
				if coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
					// Burn the stake out of the lock, so staked_dream drops
					// with the balance in one step.
					if burnErr := k.late.repKeeper.BurnLockedDREAM(ctx, endorserAddr, endorsement.DreamStake); burnErr != nil {
						sdkCtx.Logger().Error("failed to burn DREAM for endorser on hidden collection delete",
							"collection_id", coll.Id, "endorser", endorsement.Endorser, "error", burnErr)
					}
					k.deductRepPerTag(ctx, endorserAddr, coll.Tags, params.EndorserRepPenalty)
					sdkCtx.EventManager().EmitEvent(sdk.NewEvent("endorsement_stake_slashed",
						sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
						sdk.NewAttribute("endorser", endorsement.Endorser),
						sdk.NewAttribute("amount", endorsement.DreamStake.String()),
						sdk.NewAttribute("rep_penalty", params.EndorserRepPenalty.String()),
						sdk.NewAttribute("rep_penalty_tags", strings.Join(coll.Tags, ",")),
					))
				} else {
					if unlockErr := k.late.repKeeper.UnlockDREAM(ctx, endorserAddr, endorsement.DreamStake); unlockErr != nil {
						sdkCtx.Logger().Error("failed to unlock DREAM for endorser on collection delete",
							"collection_id", coll.Id, "endorser", endorsement.Endorser, "error", unlockErr)
					}
				}
			}
			endorsement.StakeReleased = true
			if err := k.Endorsement.Set(ctx, coll.Id, endorsement); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: Endorsement.Set failed", "err", err)
			}
		}
		// Remove endorsement indexes
		if err == nil {
			if err := k.EndorsementStakeExpiry.Remove(ctx, collections.Join(endorsement.StakeReleaseAt, coll.Id)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: EndorsementStakeExpiry.Remove failed", "err", err)
			}
		}
		if err := k.Endorsement.Remove(ctx, coll.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: Endorsement.Remove failed", "err", err)
		}
	}

	// Handle pending sponsorship request: refund escrowed deposits
	req, err := k.SponsorshipRequest.Get(ctx, coll.Id)
	if err == nil {
		requesterAddr, err := k.addressCodec.StringToBytes(req.Requester)
		if err == nil {
			refundAmt := req.CollectionDeposit.Add(req.ItemDepositTotal)
			if refundAmt.IsPositive() {
				if err := k.RefundSPARK(ctx, requesterAddr, refundAmt); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: k.RefundSPARK failed", "err", err)
				}
			}
		}
		if err := k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(req.ExpiresAt, coll.Id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: SponsorshipRequestsByExpiry.Remove failed", "err", err)
		}
		if err := k.SponsorshipRequest.Remove(ctx, coll.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: SponsorshipRequest.Remove failed", "err", err)
		}
	}

	// Close open hides: release the sentinel bond. An appeal in flight is
	// owned by x/rep and stays unresolved, so the verdict still settles the
	// sentinel's reservation (see closeHideRecordOnTargetDelete).
	err = k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, coll.Id)),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hr, err := k.HideRecord.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			k.closeHideRecordOnTargetDelete(ctx, hr)
			return false, nil
		},
	)
	if err != nil {
		sdkCtx.Logger().Error("deleteCollectionFull: HideRecordByTarget.Walk failed", "collection_id", coll.Id, "err", err)
	}

	// Cleanup curation: refund pending challenge deposits, release any
	// committed slash budget back to the curator's bonded role.
	err = k.CurationReviewsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](coll.Id),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			review, err := k.CurationReview.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			// Refund pending challenge deposits and release committed bond.
			if isChallengePending(review) {
				challengerAddr, aerr := k.addressCodec.StringToBytes(review.Challenger)
				if aerr == nil {
					if err := k.late.repKeeper.UnlockDREAM(ctx, challengerAddr, challengeDepositOf(review, params)); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: repKeeper.UnlockDREAM failed", "err", err)
					}
				}
				if !review.CommittedSlash.IsNil() && review.CommittedSlash.IsPositive() {
					if err := k.late.repKeeper.ReleaseBond(ctx,
						reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
						review.Curator, review.CommittedSlash); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: repKeeper.ReleaseBond failed", "err", err)
					}
				}
			}
			// Remove review indexes
			if err := k.CurationReviewsByCurator.Remove(ctx, collections.Join(review.Curator, review.Id)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: CurationReviewsByCurator.Remove failed", "err", err)
			}
			if err := k.CurationReview.Remove(ctx, review.Id); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: CurationReview.Remove failed", "err", err)
			}
			return false, nil
		},
	)
	if err != nil {
		sdkCtx.Logger().Error("deleteCollectionFull: CurationReviewsByCollection.Walk failed", "collection_id", coll.Id, "err", err)
	}
	// Remove all review-by-collection index entries
	var reviewKeys []collections.Pair[uint64, uint64]
	if err := k.CurationReviewsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](coll.Id),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			reviewKeys = append(reviewKeys, key)
			return false, nil
		},
	); err != nil {
		return err
	}
	for _, key := range reviewKeys {
		if err := k.CurationReviewsByCollection.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: CurationReviewsByCollection.Remove failed", "err", err)
		}
	}
	if err := k.CurationSummary.Remove(ctx, coll.Id); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: CurationSummary.Remove failed", "err", err)
	}

	// Delete all items
	var itemKeys []collections.Pair[uint64, uint64]
	if err := k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](coll.Id),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			itemKeys = append(itemKeys, key)
			return false, nil
		},
	); err != nil {
		return err
	}
	for _, key := range itemKeys {
		itemID := key.K2()
		item, err := k.Item.Get(ctx, itemID)
		if err == nil {
			// Clean up item hide records
			k.cleanupItemHideRecords(ctx, item, params)
			// Clean up item flags
			flagKey := FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, itemID)
			flag, err := k.Flag.Get(ctx, flagKey)
			if err == nil {
				if flag.InReviewQueue {
					if err := k.FlagReviewQueue.Remove(ctx, collections.Join(int32(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM), itemID)); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: FlagReviewQueue.Remove failed", "err", err)
					}
				}
				if err := k.FlagExpiry.Remove(ctx, collections.Join(flagExpiryBlock(flag, params), flagKey)); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: FlagExpiry.Remove failed", "err", err)
				}
				if err := k.Flag.Remove(ctx, flagKey); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: Flag.Remove failed", "err", err)
				}
			}
			// Clean up item reaction dedup entries and the on-chain ref index.
			k.deleteTargetReactions(ctx, types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, itemID)
			k.removeItemOnChainRef(ctx, item)
			if err := k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, itemID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: ItemsByOwner.Remove failed", "err", err)
			}
		}
		if err := k.Item.Remove(ctx, itemID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: Item.Remove failed", "err", err)
		}
		if err := k.ItemsByCollection.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: ItemsByCollection.Remove failed", "err", err)
		}
	}

	// Delete all collaborators by walking only this collection's key range
	// "{id}/..." of the Collaborator map (no global scan).
	collabPrefix := fmt.Sprintf("%d/", coll.Id)
	type collabEntry struct {
		key    string
		record types.Collaborator
	}
	var collabEntries []collabEntry
	if err := k.Collaborator.Walk(ctx, collaboratorRange(coll.Id),
		func(key string, record types.Collaborator) (bool, error) {
			collabEntries = append(collabEntries, collabEntry{key: key, record: record})
			return false, nil
		},
	); err != nil {
		return err
	}
	for _, entry := range collabEntries {
		// Settle non-member collaborator stake first (HIDDEN → fractional
		// burn; otherwise full refund). Failures are logged inside the helper
		// and never block deletion.
		_, _, _ = k.releaseOrSlashCollabStake(ctx, coll, entry.record)
		// Extract address from composite key "collectionID/address"
		addr := entry.key[len(collabPrefix):]
		if err := k.Collaborator.Remove(ctx, entry.key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: Collaborator.Remove failed", "err", err)
		}
		if err := k.CollaboratorReverse.Remove(ctx, collections.Join(addr, coll.Id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: CollaboratorReverse.Remove failed", "err", err)
		}
	}

	// Drop the collection's reaction dedup markers.
	k.deleteTargetReactions(ctx, types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, coll.Id)

	// Clean up collection-level flags
	flagKey := FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, coll.Id)
	flag, err := k.Flag.Get(ctx, flagKey)
	if err == nil {
		if flag.InReviewQueue {
			if err := k.FlagReviewQueue.Remove(ctx, collections.Join(int32(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION), coll.Id)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: FlagReviewQueue.Remove failed", "err", err)
			}
		}
		if err := k.FlagExpiry.Remove(ctx, collections.Join(flagExpiryBlock(flag, params), flagKey)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: FlagExpiry.Remove failed", "err", err)
		}
		if err := k.Flag.Remove(ctx, flagKey); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: Flag.Remove failed", "err", err)
		}
	}

	// Clean up hide records for collection
	var hideTargetKeys []collections.Pair[string, uint64]
	collTargetKey := HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, coll.Id)
	if err := k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](collTargetKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hideTargetKeys = append(hideTargetKeys, key)
			return false, nil
		},
	); err != nil {
		return err
	}
	for _, key := range hideTargetKeys {
		if err := k.HideRecordByTarget.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: HideRecordByTarget.Remove failed", "err", err)
		}
	}

	// Remove initiative link if collection references an initiative (best effort)
	if coll.InitiativeId > 0 && k.late.repKeeper != nil {
		if linkErr := k.late.repKeeper.RemoveContentInitiativeLink(ctx, coll.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT), coll.Id); linkErr != nil {
			sdkCtx.Logger().Error("failed to remove content initiative link on delete", "collection_id", coll.Id, "error", linkErr)
		}
	}

	// Remove indexes
	if err := k.CollectionsByOwner.Remove(ctx, collections.Join(coll.Owner, coll.Id)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: CollectionsByOwner.Remove failed", "err", err)
	}
	if coll.ExpiresAt > 0 {
		if err := k.CollectionsByExpiry.Remove(ctx, collections.Join(coll.ExpiresAt, coll.Id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: CollectionsByExpiry.Remove failed", "err", err)
		}
	}
	k.RemoveCollectionStatusIndex(ctx, coll.Status, coll.Pinned, coll.Id)
	// Remove tag secondary index entries for this collection AND drop the
	// rep-registry UsageCount for each tag — the collection is going away so
	// it no longer "uses" them. Used by both MsgDeleteCollection and the
	// EndBlocker TTL/endorsement-expiry prunes (both reach this helper).
	for _, tag := range coll.Tags {
		if err := k.CollectionsByTag.Remove(ctx, collections.Join(tag, coll.Id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: CollectionsByTag.Remove failed", "err", err)
		}
	}
	k.decrementTagUsages(ctx, coll.Tags)

	// Delete collection
	if err := k.Collection.Remove(ctx, coll.Id); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("deleteCollectionFull: Collection.Remove failed", "err", err)
	}

	depositRefunded := "0"
	itemDepositRefunded := "0"
	if !coll.DepositBurned {
		depositRefunded = coll.DepositAmount.String()
		itemDepositRefunded = coll.ItemDepositTotal.String()
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_deleted",
		sdk.NewAttribute("id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("owner", coll.Owner),
		sdk.NewAttribute("item_count", strconv.FormatUint(coll.ItemCount, 10)),
		sdk.NewAttribute("deposit_refunded", depositRefunded),
		sdk.NewAttribute("item_deposit_refunded", itemDepositRefunded),
	))

	return nil
}

// cleanupItemHideRecords handles hide records for an item being deleted:
// closes them and drops their by-target index entries, as the collection
// path does (the records themselves stay for the sentinel's history).
func (k Keeper) cleanupItemHideRecords(ctx context.Context, item types.Item, params types.Params) {
	targetKey := HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, item.Id)
	var keys []collections.Pair[string, uint64]
	if err := k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](targetKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			keys = append(keys, key)
			return false, nil
		},
	); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupItemHideRecords: HideRecordByTarget.Walk failed", "err", err)
	}
	for _, key := range keys {
		if hr, err := k.HideRecord.Get(ctx, key.K2()); err == nil {
			k.closeHideRecordOnTargetDelete(ctx, hr)
		}
		if err := k.HideRecordByTarget.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupItemHideRecords: HideRecordByTarget.Remove failed", "err", err)
		}
	}
}

// countCollectionsByOwner counts how many collections an owner has.
func (k Keeper) countCollectionsByOwner(ctx context.Context, owner string) (uint32, error) {
	var count uint32
	err := k.CollectionsByOwner.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](owner),
		func(key collections.Pair[string, uint64]) (bool, error) {
			count++
			return false, nil
		},
	)
	return count, err
}

// getMaxCollections returns the tiered collection limit for an address.
func (k Keeper) getMaxCollections(ctx context.Context, address string, params types.Params) uint32 {
	if k.late.repKeeper == nil {
		return params.MaxCollectionsBase
	}
	addrBytes, err := k.addressCodec.StringToBytes(address)
	if err != nil {
		return params.MaxCollectionsBase
	}
	if !k.late.repKeeper.IsActiveMember(ctx, addrBytes) {
		return params.MaxCollectionsBase
	}
	tl, err := k.late.repKeeper.GetTrustLevel(ctx, addrBytes)
	if err != nil {
		return params.MaxCollectionsBase
	}
	return params.MaxCollectionsBase + commontypes.Saturate[uint32](TrustLevelIndex(tl))*params.MaxCollectionsPerTrustLevel
}

// isAnonymous reports whether address is the shield module account, i.e. an
// anonymous action routed through x/shield. Every anonymous member shares that
// address, so per-address bookkeeping (one vote per voter, daily vote limits,
// "own content" checks) must not apply to it: x/shield's nullifier allows one
// action per member per target, and its identity rate limit bounds each member.
func (k Keeper) isAnonymous(address string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(address)
	return err == nil && sdk.AccAddress(addrBytes).Equals(shieldModuleAddress)
}

// isCollectionOwner reports whether address owns coll. The shield address owns
// every anonymous collection, so for it ownership also takes an x/shield
// ownership proof for this collection's owner tag in the context; without one
// the shield address owns nothing.
func (k Keeper) isCollectionOwner(ctx context.Context, coll types.Collection, address string) bool {
	if !k.canActAs(ctx, address, coll.Owner) {
		return false
	}
	if !k.isAnonymous(address) {
		return true
	}
	tag, ok := shieldtypes.GetOwnershipTag(ctx)
	return ok && len(coll.AnonOwnerTag) > 0 && bytes.Equal(tag, coll.AnonOwnerTag)
}

// isMember checks if the address is an active x/rep member.
// The shield module address is always considered a member because the ZK proof
// verified membership before routing the message.
func (k Keeper) isMember(ctx context.Context, address string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(address)
	if err != nil {
		return false
	}
	if sdk.AccAddress(addrBytes).Equals(shieldModuleAddress) {
		return true
	}
	return k.late.repKeeper.IsActiveMember(ctx, addrBytes)
}

// ownsAsMember reports whether a collection owner gets member treatment
// (ACTIVE status, permanent collections): an x/rep member, or a council or
// committee policy address acting for the community (the Operations
// Committee owns a federation peer's author-curation list). Only ownership:
// voting, flagging and the like stay member-only.
func (k Keeper) ownsAsMember(ctx context.Context, address string) bool {
	if k.isMember(ctx, address) {
		return true
	}
	return k.commonsKeeper != nil && k.commonsKeeper.IsGroupPolicyAddress(ctx, address)
}

// meetsMinTrustLevel checks if address is at or above the required trust level string.
// The shield module address is compared using the level its ZK proof established
// (shieldtypes.ProvenTrustLevel); with no proven level it is refused.
func (k Keeper) meetsMinTrustLevel(ctx context.Context, address string, minLevel string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(address)
	if err != nil {
		return false
	}
	if sdk.AccAddress(addrBytes).Equals(shieldModuleAddress) {
		// Anonymous: compare with the level the ZK proof established. No
		// proven level means the message didn't come through a shield exec,
		// so nothing about the actual signer is known: refuse.
		proven, ok := shieldtypes.ProvenTrustLevel(ctx)
		if !ok {
			return false
		}
		required, ok := ParseTrustLevel(minLevel)
		return ok && int64(proven) >= int64(required)
	}
	if !k.late.repKeeper.IsActiveMember(ctx, addrBytes) {
		return false
	}
	tl, err := k.late.repKeeper.GetTrustLevel(ctx, addrBytes)
	if err != nil {
		return false
	}
	required, ok := ParseTrustLevel(minLevel)
	if !ok {
		return false
	}
	return tl >= required
}

// checkDailyLimit checks and increments a daily reaction counter. Returns error if limit exceeded.
func (k Keeper) checkDailyLimit(ctx context.Context, address string, blockHeight int64, category string, maxPerDay uint32) error {
	key := ReactionLimitCompositeKey(address, blockHeight, category)
	current, err := k.ReactionLimit.Get(ctx, key)
	if err != nil {
		current = 0
	}
	if current >= maxPerDay {
		return types.ErrMaxDailyReactions
	}
	return k.ReactionLimit.Set(ctx, key, current+1)
}

// attrsToValues converts a slice of KeyValuePair pointers to values.
func attrsToValues(attrs []*types.KeyValuePair) []types.KeyValuePair {
	result := make([]types.KeyValuePair, len(attrs))
	for i, a := range attrs {
		if a != nil {
			result[i] = *a
		}
	}
	return result
}

// isTTLCollection returns true if the collection has a TTL and deposits are held (not burned).
func isTTLCollection(coll types.Collection) bool {
	return coll.ExpiresAt > 0 && !coll.DepositBurned
}

// chargesDeposits reports whether the owner of coll pays SPARK deposits,
// fees and spam tax. An anonymous collection is owned by the shield module
// account, whose balance is the communal gas reserve rather than the
// member's money, so anonymous collections and their items carry none;
// anonymous volume is bounded by x/shield's per-op rate-limit windows and
// the per-collection item cap instead.
func (k Keeper) chargesDeposits(coll types.Collection) bool {
	return !k.isAnonymous(coll.Owner)
}

// heldItemDeposit returns the item deposit to release when n items leave a
// TTL collection: n * perItem, capped at the ItemDepositTotal actually held so
// a param change or an anonymous (deposit-free) collection never releases
// SPARK that was not collected.
func heldItemDeposit(coll types.Collection, perItem math.Int, n uint64) math.Int {
	if coll.ItemDepositTotal.IsNil() || !coll.ItemDepositTotal.IsPositive() || perItem.IsNil() || !perItem.IsPositive() {
		return math.ZeroInt()
	}
	return math.MinInt(perItem.MulRaw(commontypes.Saturate[int64](n)), coll.ItemDepositTotal)
}

// decrementItemCount saturates at zero to avoid uint64 underflow when a
// collection's item counter has drifted below the decrement amount.
func (k Keeper) decrementItemCount(coll *types.Collection, n uint64) {
	if coll.ItemCount < n {
		coll.ItemCount = 0
	} else {
		coll.ItemCount -= n
	}
}

// validateReferenceFields validates that the correct reference is set for the given type
// and all string fields are within length limits.
func (k Keeper) validateReferenceFields(msg_refType types.ReferenceType, nft *types.NftReference, link *types.LinkReference, onChain *types.OnChainReference, custom *types.CustomReference, maxLen uint32) error {
	switch msg_refType {
	case types.ReferenceType_REFERENCE_TYPE_NFT:
		if nft == nil {
			return types.ErrInvalidReference
		}
		if commontypes.Saturate[uint32](len(nft.ChainId)) > maxLen || commontypes.Saturate[uint32](len(nft.ContractAddress)) > maxLen ||
			commontypes.Saturate[uint32](len(nft.TokenId)) > maxLen || commontypes.Saturate[uint32](len(nft.TokenStandard)) > maxLen ||
			commontypes.Saturate[uint32](len(nft.TokenUri)) > maxLen {
			return types.ErrReferenceFieldTooLong
		}
	case types.ReferenceType_REFERENCE_TYPE_LINK:
		if link == nil {
			return types.ErrInvalidReference
		}
		if commontypes.Saturate[uint32](len(link.Uri)) > maxLen || commontypes.Saturate[uint32](len(link.ContentHash)) > maxLen ||
			commontypes.Saturate[uint32](len(link.ContentType)) > maxLen {
			return types.ErrReferenceFieldTooLong
		}
	case types.ReferenceType_REFERENCE_TYPE_ON_CHAIN:
		if onChain == nil {
			return types.ErrInvalidReference
		}
		if commontypes.Saturate[uint32](len(onChain.Module)) > maxLen || commontypes.Saturate[uint32](len(onChain.EntityType)) > maxLen ||
			commontypes.Saturate[uint32](len(onChain.EntityId)) > maxLen {
			return types.ErrReferenceFieldTooLong
		}
	case types.ReferenceType_REFERENCE_TYPE_CUSTOM:
		if custom == nil {
			return types.ErrInvalidReference
		}
		if commontypes.Saturate[uint32](len(custom.TypeLabel)) > maxLen || commontypes.Saturate[uint32](len(custom.Value)) > maxLen {
			return types.ErrReferenceFieldTooLong
		}
	case types.ReferenceType_REFERENCE_TYPE_UNSPECIFIED:
		// OK - no reference needed
	default:
		return types.ErrInvalidReference
	}
	return nil
}

// validateItemFields validates item content fields against params.
func (k Keeper) validateItemFields(encrypted bool, title, description, imageUri string, refType types.ReferenceType, nft *types.NftReference, link *types.LinkReference, onChain *types.OnChainReference, custom *types.CustomReference, attributes []types.KeyValuePair, encryptedData []byte, params types.Params) error {
	if encrypted {
		if commontypes.Saturate[uint32](len(encryptedData)) > params.MaxEncryptedDataSize {
			return types.ErrEncryptedDataTooLarge
		}
		if title != "" || description != "" || imageUri != "" {
			return types.ErrEncryptedFieldMismatch
		}
		return nil
	}

	// Public item validation
	if len(encryptedData) > 0 {
		return types.ErrEncryptedFieldMismatch
	}
	if commontypes.Saturate[uint32](len(title)) > params.MaxTitleLength {
		return types.ErrInvalidTitle
	}
	if commontypes.Saturate[uint32](len(description)) > params.MaxDescriptionLength {
		return types.ErrInvalidDescription
	}
	if commontypes.Saturate[uint32](len(imageUri)) > params.MaxReferenceFieldLength {
		return types.ErrReferenceFieldTooLong
	}

	if err := k.validateReferenceFields(refType, nft, link, onChain, custom, params.MaxReferenceFieldLength); err != nil {
		return err
	}
	if err := rejectDataURIs(itemDataURIFields(imageUri, nft, link, custom, attributes)...); err != nil {
		return err
	}

	// Count attributes (including custom reference extra)
	attrCount := commontypes.Saturate[uint32](len(attributes))
	if custom != nil {
		attrCount += commontypes.Saturate[uint32](len(custom.Extra))
	}
	if attrCount > params.MaxAttributesPerItem {
		return types.ErrMaxAttributes
	}
	for _, attr := range attributes {
		if commontypes.Saturate[uint32](len(attr.Key)) > params.MaxAttributeKeyLength {
			return types.ErrAttributeTooLong
		}
		if commontypes.Saturate[uint32](len(attr.Value)) > params.MaxAttributeValueLength {
			return types.ErrAttributeTooLong
		}
	}
	if custom != nil {
		for _, extra := range custom.Extra {
			if commontypes.Saturate[uint32](len(extra.Key)) > params.MaxAttributeKeyLength {
				return types.ErrAttributeTooLong
			}
			if commontypes.Saturate[uint32](len(extra.Value)) > params.MaxAttributeValueLength {
				return types.ErrAttributeTooLong
			}
		}
	}

	return nil
}

// supersedeSponsorshipRequest cancels a pending sponsorship request on a
// collection that became permanent another way: the escrowed permanent
// deposits go back to the requester and the request and its expiry index
// entry are deleted. No request is a no-op.
func (k Keeper) supersedeSponsorshipRequest(ctx context.Context, collID uint64) error {
	req, err := k.SponsorshipRequest.Get(ctx, collID)
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	refundAmt := req.CollectionDeposit.Add(req.ItemDepositTotal)
	if refundAmt.IsPositive() {
		requesterAddr, err := k.addressCodec.StringToBytes(req.Requester)
		if err != nil {
			return errorsmod.Wrap(err, "invalid sponsorship requester address")
		}
		if err := k.RefundSPARK(ctx, requesterAddr, refundAmt); err != nil {
			return err
		}
	}
	if err := k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(req.ExpiresAt, collID)); err != nil {
		return err
	}
	return k.SponsorshipRequest.Remove(ctx, collID)
}
