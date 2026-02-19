package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestPruneExpiredCollections(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100 so expiry math works
	f.setBlockHeight(100)

	// Create a TTL collection that expires at block 200
	collID := f.createTTLCollection(t, f.owner, 200)

	// Verify it exists
	_, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)

	// Track deposit refund
	var refundCalled bool
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		refundCalled = true
		return nil
	}

	// Advance past expiry
	f.setBlockHeight(201)

	// Run PruneExpired
	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify collection is deleted
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify deposit refund was called
	require.True(t, refundCalled)
}

func TestPruneSponsorshipRequests(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create a TTL collection as nonMember (creates PENDING collection)
	collID := f.createPendingCollection(t)

	// Request sponsorship
	_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
		Creator:      f.nonMember,
		CollectionId: collID,
	})
	require.NoError(t, err)

	// Verify sponsorship request exists
	_, err = f.keeper.SponsorshipRequest.Get(f.ctx, collID)
	require.NoError(t, err)

	// Track refund
	var refundCount int
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		refundCount++
		return nil
	}

	// Advance past sponsorship request TTL (default 100800 blocks)
	// The request was created at block 100, expires at 100 + 100800 = 100900
	f.setBlockHeight(100901)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify sponsorship request is deleted
	_, err = f.keeper.SponsorshipRequest.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify refund was called (deposits refunded)
	require.Greater(t, refundCount, 0)
}

func TestPruneUnappealedHides(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Hide it (sentinel)
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: types.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hideRecordID := resp.HideRecordId

	// Verify collection is HIDDEN
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_HIDDEN, coll.Status)

	// Track bond release
	var bondReleased bool
	f.forumKeeper.releaseBondCommitmentFn = func(_ context.Context, _ string, _ math.Int, _ string, _ uint64) error {
		bondReleased = true
		return nil
	}

	// Advance past hide_expiry_blocks (default 100800)
	// Hidden at block 100, appeal deadline = 100 + 100800 = 100900
	f.setBlockHeight(100901)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify hide record is resolved
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)

	// Verify collection is deleted (unappealed hide = content deleted)
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify sentinel bond was released
	require.True(t, bondReleased)
}

func TestPruneAppealTimeouts(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Hide it (sentinel)
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: types.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hideRecordID := resp.HideRecordId

	// Advance past appeal cooldown (default 600 blocks)
	f.advanceBlockHeight(601)

	// Appeal the hide (owner)
	_, err = f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
		Creator:      f.owner,
		HideRecordId: hideRecordID,
	})
	require.NoError(t, err)

	// Verify appealed
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Appealed)

	// Track refund and burn
	var refundCalled bool
	var burnCalled bool
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		refundCalled = true
		return nil
	}
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	// Track bond release
	var bondReleased bool
	f.forumKeeper.releaseBondCommitmentFn = func(_ context.Context, _ string, _ math.Int, _ string, _ uint64) error {
		bondReleased = true
		return nil
	}

	// Advance past appeal_deadline_blocks (default 201600)
	// Appeal was filed, new deadline = current_block + 201600
	f.advanceBlockHeight(201601)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify hide record is resolved
	hr, err = f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)

	// Verify collection is restored to ACTIVE (appeal timeout favors appellant)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)

	// Verify refund and burn happened (50% refund, 50% burn)
	require.True(t, refundCalled)
	require.True(t, burnCalled)

	// Verify sentinel bond released
	require.True(t, bondReleased)
}

func TestPruneExpiredFlags(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Flag it (member)
	_, err := f.msgServer.FlagContent(f.ctx, &types.MsgFlagContent{
		Creator:    f.member,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		Reason:     types.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)

	// Verify flag exists
	actualFlagKey := fmtFlagKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID)
	_, err = f.keeper.Flag.Get(f.ctx, actualFlagKey)
	require.NoError(t, err)

	// Advance past flag_expiration_blocks (default 100800)
	// Flag was created at block 100, expiry = 100 + 100800 = 100900
	f.setBlockHeight(100901)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify flag is removed
	_, err = f.keeper.Flag.Get(f.ctx, actualFlagKey)
	require.Error(t, err)
}

func TestPruneUnendorsedCollections(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create a PENDING collection (nonMember)
	collID := f.createPendingCollection(t)

	// Verify it exists and is PENDING
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_PENDING, coll.Status)

	// Track refunds
	var refundCount int
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		refundCount++
		return nil
	}

	// Advance past endorsement_expiry_blocks (default 432000)
	// Created at block 100, endorsement pending expiry = 100 + 432000 = 432100
	f.setBlockHeight(432101)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify collection is deleted
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify refunds were called (endorsement creation fee + deposit)
	require.Greater(t, refundCount, 0)
}

func TestReleaseEndorsementStakes(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create a PENDING collection via nonMember with a very long TTL
	// so the collection doesn't expire before the endorsement stake duration.
	// max_non_member_ttl_blocks = 432000, stake_duration = 432000, so set TTL = blockHeight + 432000
	msg := &types.MsgCreateCollection{
		Creator:    f.nonMember,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "endorse-stake-test",
		ExpiresAt:  100 + 432000, // maximum allowed for non-members
	}
	resp, err := f.msgServer.CreateCollection(f.ctx, msg)
	require.NoError(t, err)
	collID := resp.Id

	// Set seeking_endorsement = true
	_, err = f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
		Creator:      f.nonMember,
		CollectionId: collID,
		Seeking:      true,
	})
	require.NoError(t, err)

	// Endorse it (member) — endorsement changes status to ACTIVE
	_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
		Creator:      f.member,
		CollectionId: collID,
	})
	require.NoError(t, err)

	// After endorsement, the collection is ACTIVE but still has a TTL.
	// The endorsement stake release is at block 100 + 432000 = 432100.
	// The TTL expiry is also at 432100. We need to remove the expiry index
	// so pruneExpiredCollections doesn't delete the collection first.
	// Or, we can simply test by advancing to exactly the stake release block
	// and verifying PruneExpired releases the stake.
	// Since both expire at 432100, the TTL prune will run first and delete the collection,
	// which also releases the endorsement stake via deleteCollectionFull.
	// So let's verify that the unlock is called through the TTL expiry path.

	// Verify endorsement exists
	endorsement, err := f.keeper.Endorsement.Get(f.ctx, collID)
	require.NoError(t, err)
	require.False(t, endorsement.StakeReleased)

	// Track DREAM unlock
	var unlockCalled bool
	var unlockAddr sdk.AccAddress
	var unlockAmount math.Int
	f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		unlockCalled = true
		unlockAddr = addr
		unlockAmount = amount
		return nil
	}

	// Advance past both TTL and stake duration (both at 432100)
	f.setBlockHeight(432101)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify DREAM was unlocked (either by deleteCollectionFull or releaseExpiredEndorsementStakes)
	require.True(t, unlockCalled)
	require.Equal(t, f.memberAddr.Bytes(), unlockAddr.Bytes())
	require.Equal(t, types.DefaultEndorsementDreamStake, unlockAmount)
}

func TestMaxPrunePerBlock(t *testing.T) {
	f := initTestFixture(t)

	// Set MaxPrunePerBlock to a small number
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPrunePerBlock = 3
	err = f.keeper.Params.Set(f.ctx, params)
	require.NoError(t, err)

	// Start at block 100
	f.setBlockHeight(100)

	// Create 5 TTL collections that all expire at block 200
	var collIDs []uint64
	for i := 0; i < 5; i++ {
		collID := f.createTTLCollection(t, f.owner, 200)
		collIDs = append(collIDs, collID)
	}

	// Advance past expiry
	f.setBlockHeight(201)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Count how many collections remain (cap=3, each 0-item collection costs 1)
	var remaining int
	for _, collID := range collIDs {
		_, err := f.keeper.Collection.Get(f.ctx, collID)
		if err == nil {
			remaining++
		}
	}
	require.Equal(t, 2, remaining, "MaxPrunePerBlock=3 should leave 2 of 5 collections un-pruned")

	// Run PruneExpired again to prune the rest
	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	remaining = 0
	for _, collID := range collIDs {
		_, err := f.keeper.Collection.Get(f.ctx, collID)
		if err == nil {
			remaining++
		}
	}
	require.Equal(t, 0, remaining, "second pass should prune remaining collections")
}

// fmtFlagKey mirrors keeper.FlagCompositeKey without importing the internal package.
func fmtFlagKey(targetType types.FlagTargetType, targetID uint64) string {
	return fmt.Sprintf("%d/%d", int32(targetType), targetID)
}

// Verify the EndorsementPending index is cleaned up on endorsement
func TestEndorsementPendingCleanedUpOnEndorse(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createPendingCollection(t)

	// Verify EndorsementPending has an entry
	var pendingCount int
	f.keeper.EndorsementPending.Walk(f.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K2() == collID {
			pendingCount++
		}
		return false, nil
	})
	require.Equal(t, 1, pendingCount)

	// Set seeking and endorse
	_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
		Creator: f.nonMember, CollectionId: collID, Seeking: true,
	})
	require.NoError(t, err)
	_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
		Creator: f.member, CollectionId: collID,
	})
	require.NoError(t, err)

	// Verify EndorsementPending entry is removed
	pendingCount = 0
	f.keeper.EndorsementPending.Walk(f.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K2() == collID {
			pendingCount++
		}
		return false, nil
	})
	require.Equal(t, 0, pendingCount)
}
