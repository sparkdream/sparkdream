package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// nonMemberOnlyOwnerAndMember restricts membership to {owner, member,
// sentinel} so the test can treat nonMember as an actual non-member.
func nonMemberOnlyOwnerAndMember(f *testFixture) func(context.Context, sdk.AccAddress) bool {
	return func(_ context.Context, addr sdk.AccAddress) bool {
		return addr.Equals(f.ownerAddr) || addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
	}
}

// recordDREAMCalls wires the mock rep keeper to record all lock/unlock/burn
// calls without rejecting them. Tests inspect the recorded calls to assert
// stake flow.
func recordDREAMCalls(f *testFixture) {
	f.repKeeper.lockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		f.repKeeper.lockCalls = append(f.repKeeper.lockCalls, dreamCall{addr: addr, amount: amount})
		return nil
	}
	f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		f.repKeeper.unlockCalls = append(f.repKeeper.unlockCalls, dreamCall{addr: addr, amount: amount})
		return nil
	}
	f.repKeeper.burnDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		f.repKeeper.burnCalls = append(f.repKeeper.burnCalls, dreamCall{addr: addr, amount: amount})
		return nil
	}
}

func TestAfterMemberAdmitted_EnqueuesAddress(t *testing.T) {
	f := initTestFixture(t)

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.nonMemberAddr))

	require.True(t, f.keeper.IsMemberQueuedForPromotion(f.ctx, f.nonMember),
		"newly-admitted address should be enqueued for promotion")
}

func TestDrainPromotionQueue_RefundsInviterStake(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)
	recordDREAMCalls(f)

	// Owner adds nonMember as a collaborator — owner locks stake.
	collID := f.createCollection(t, f.owner)
	_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
		Creator:      f.owner,
		CollectionId: collID,
		Address:      f.nonMember,
		Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
	})
	require.NoError(t, err)

	params, _ := f.keeper.Params.Get(f.ctx)
	require.Len(t, f.repKeeper.lockCalls, 1, "owner must have locked stake on add")
	require.True(t, f.repKeeper.lockCalls[0].amount.Equal(params.NonMemberCollabDreamStake))

	// nonMember now admitted: hook + EndBlocker drain.
	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.nonMemberAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Inviter's stake refunded in full.
	require.Len(t, f.repKeeper.unlockCalls, 1)
	require.True(t, f.repKeeper.unlockCalls[0].addr.Equals(f.ownerAddr))
	require.True(t, f.repKeeper.unlockCalls[0].amount.Equal(params.NonMemberCollabDreamStake))
	require.Empty(t, f.repKeeper.burnCalls, "admission must not burn")

	// Collaborator record stripped of stake bookkeeping; non-member counter
	// decremented.
	collab, err := f.keeper.Collaborator.Get(f.ctx, keeper.CollaboratorCompositeKey(collID, f.nonMember))
	require.NoError(t, err)
	require.Equal(t, "", collab.Inviter)
	require.True(t, collab.DreamStake.IsZero())

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, uint32(0), coll.NonMemberCollaboratorCount)
	require.Equal(t, uint32(1), coll.CollaboratorCount, "collaborator stays a collaborator (now as a member)")

	// Queue drained.
	require.False(t, f.keeper.IsMemberQueuedForPromotion(f.ctx, f.nonMember))
}

func TestDrainPromotionQueue_PromotesPendingCollection(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)
	recordDREAMCalls(f)

	collID := f.createPendingCollection(t)

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_PENDING, coll.Status)
	require.True(t, coll.ExpiresAt > 0)
	oldExpiresAt := coll.ExpiresAt

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.nonMemberAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	coll, _ = f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status, "PENDING → ACTIVE")
	require.Equal(t, int64(0), coll.ExpiresAt, "expires_at cleared")
	require.True(t, coll.DepositBurned, "deposit_burned set")
	require.False(t, coll.SeekingEndorsement, "seeking_endorsement cleared")

	// CollectionsByExpiry entry removed.
	hasExpiry, _ := f.keeper.CollectionsByExpiry.Has(f.ctx, collections.Join(oldExpiresAt, collID))
	require.False(t, hasExpiry)

	// Status index swapped. Key is (status, pinned-rank, id); the collection is
	// unpinned throughout, so pinned-rank is 1.
	hasPending, _ := f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(int32(types.CollectionStatus_COLLECTION_STATUS_PENDING), int32(1), collID))
	hasActive, _ := f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE), int32(1), collID))
	require.False(t, hasPending)
	require.True(t, hasActive)
}

func TestDrainPromotionQueue_PromotesActiveTTLCollection(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)
	recordDREAMCalls(f)

	// member-owned ACTIVE+TTL collection — created while owner was a member
	// (here owner == owner var) but then enqueue a "newly admitted" event
	// for owner to exercise the promotion path on already-active+TTL state.
	expiresAt := sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 50000
	collID := f.createTTLCollection(t, f.owner, expiresAt)

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.ownerAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, int64(0), coll.ExpiresAt, "ACTIVE+TTL → permanent")
	require.True(t, coll.DepositBurned)
}

func TestDrainPromotionQueue_SkipsHiddenCollection(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)
	recordDREAMCalls(f)

	expiresAt := sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 50000
	collID := f.createTTLCollection(t, f.owner, expiresAt)

	// Flip to HIDDEN before the drain.
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
	require.NoError(t, f.keeper.Collection.Set(f.ctx, collID, coll))

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.ownerAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	coll, _ = f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, expiresAt, coll.ExpiresAt, "HIDDEN collection must not be promoted")
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_HIDDEN, coll.Status)
}

func TestDrainPromotionQueue_PromotesEndorsedCollection_ReleasesEndorserStake(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)
	recordDREAMCalls(f)

	// nonMember creates PENDING TTL collection; member endorses it.
	collID := f.createPendingCollection(t)
	_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
		Creator:      f.nonMember,
		CollectionId: collID,
		Seeking:      true,
	})
	require.NoError(t, err)

	_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
		Creator:      f.member,
		CollectionId: collID,
	})
	require.NoError(t, err)

	// Sanity: endorsement created and stake locked.
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	require.True(t, coll.Immutable)
	require.True(t, coll.ExpiresAt > 0, "still TTL after endorsement")
	require.Equal(t, f.member, coll.EndorsedBy)

	endorsement, err := f.keeper.Endorsement.Get(f.ctx, collID)
	require.NoError(t, err)
	require.True(t, endorsement.DreamStake.IsPositive())

	// Reset the call recorders so we only see what the drain triggers.
	f.repKeeper.lockCalls = nil
	f.repKeeper.unlockCalls = nil
	f.repKeeper.burnCalls = nil

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.nonMemberAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Collection promoted to permanent, immutable flag preserved (endorsement
	// editing lock survives promotion).
	coll, _ = f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, int64(0), coll.ExpiresAt)
	require.True(t, coll.DepositBurned)
	require.True(t, coll.Immutable, "endorsed collection stays immutable after auto-promotion")

	// Endorser's stake fully unlocked, no burn.
	require.Len(t, f.repKeeper.unlockCalls, 1)
	require.True(t, f.repKeeper.unlockCalls[0].addr.Equals(f.memberAddr))
	require.True(t, f.repKeeper.unlockCalls[0].amount.Equal(endorsement.DreamStake))
	require.Empty(t, f.repKeeper.burnCalls, "admission must not burn endorser stake")

	// Endorsement record marked released; expiry index gone.
	endorsement, err = f.keeper.Endorsement.Get(f.ctx, collID)
	require.NoError(t, err)
	require.True(t, endorsement.StakeReleased)
	hasStakeExpiry, _ := f.keeper.EndorsementStakeExpiry.Has(f.ctx, collections.Join(endorsement.StakeReleaseAt, collID))
	require.False(t, hasStakeExpiry)
}

func TestDrainPromotionQueue_MixedWorkSharesBudget(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)
	recordDREAMCalls(f)

	// nonMember is collaborator on a member-owned collection (inviter
	// stake locked) AND owns its own PENDING TTL collection.
	memberColl := f.createCollection(t, f.owner)
	_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
		Creator:      f.owner,
		CollectionId: memberColl,
		Address:      f.nonMember,
		Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
	})
	require.NoError(t, err)

	ownedColl := f.createPendingCollection(t)

	// Tighten cap to 1 so the budget is exhausted after one unit of work.
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxPromotionsPerBlock = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Reset recorders to isolate drain calls.
	f.repKeeper.lockCalls = nil
	f.repKeeper.unlockCalls = nil

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.nonMemberAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Pass 1 runs first — stake refund consumes the budget. The owned
	// PENDING collection is NOT yet promoted; address stays queued.
	require.Len(t, f.repKeeper.unlockCalls, 1, "exactly one unit of work consumed")
	coll, _ := f.keeper.Collection.Get(f.ctx, ownedColl)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_PENDING, coll.Status,
		"owned collection still PENDING — budget was spent on stake refund")
	require.True(t, f.keeper.IsMemberQueuedForPromotion(f.ctx, f.nonMember),
		"address stays in queue when pass 2 was starved by the cap")

	// Second block — pass 2 picks up where we left off.
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	coll, _ = f.keeper.Collection.Get(f.ctx, ownedColl)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status,
		"owned collection promoted in second block")
	require.Equal(t, int64(0), coll.ExpiresAt)
	require.False(t, f.keeper.IsMemberQueuedForPromotion(f.ctx, f.nonMember),
		"queue drained after both passes complete")
}

func TestDrainPromotionQueue_AlreadyPermanentIsNoOp(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)
	recordDREAMCalls(f)

	// Owner has only a permanent collection.
	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, int64(0), coll.ExpiresAt, "fixture must be permanent")

	depositBurnedBefore := coll.DepositBurned

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.ownerAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	coll, _ = f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, int64(0), coll.ExpiresAt, "still permanent")
	require.Equal(t, depositBurnedBefore, coll.DepositBurned, "DepositBurned untouched")
	require.False(t, f.keeper.IsMemberQueuedForPromotion(f.ctx, f.owner),
		"queue drains cleanly even with no real work")
}

func TestDrainPromotionQueue_StaleOwnerIndexEntryCleaned(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)

	// Hand-seed a CollectionsByOwner entry pointing at a non-existent
	// collection id (could happen post-delete if cleanup raced).
	require.NoError(t, f.keeper.CollectionsByOwner.Set(f.ctx, collections.Join(f.owner, uint64(9999))))

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.ownerAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Drain shouldn't blow up; queue should clear since there's no real work.
	require.False(t, f.keeper.IsMemberQueuedForPromotion(f.ctx, f.owner))
}

func TestDrainPromotionQueue_PerBlockCapPreservesProgress(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = nonMemberOnlyOwnerAndMember(f)
	recordDREAMCalls(f)

	// Owner owns three ACTIVE+TTL collections.
	expiresAt := sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 50000
	collA := f.createTTLCollection(t, f.owner, expiresAt)
	collB := f.createTTLCollection(t, f.owner, expiresAt)
	collC := f.createTTLCollection(t, f.owner, expiresAt)

	// Tighten the cap to 2 — first drain should promote two and leave one.
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxPromotionsPerBlock = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	hook := keeper.NewCollectRepHooks(&f.keeper)
	require.NoError(t, hook.AfterMemberAdmitted(f.ctx, f.ownerAddr))
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// CollectionsByOwner walks in (owner, id) ascending order — first two
	// promoted.
	promoted := 0
	for _, id := range []uint64{collA, collB, collC} {
		c, _ := f.keeper.Collection.Get(f.ctx, id)
		if c.ExpiresAt == 0 {
			promoted++
		}
	}
	require.Equal(t, 2, promoted, "exactly cap=2 promotions per block")
	require.True(t, f.keeper.IsMemberQueuedForPromotion(f.ctx, f.owner),
		"owner stays queued for next block to finish the remaining collection")

	// Second block drains the remainder and removes from queue.
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	for _, id := range []uint64{collA, collB, collC} {
		c, _ := f.keeper.Collection.Get(f.ctx, id)
		require.Equal(t, int64(0), c.ExpiresAt, "all three permanent after second block")
	}
	require.False(t, f.keeper.IsMemberQueuedForPromotion(f.ctx, f.owner))
}
