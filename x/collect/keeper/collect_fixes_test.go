package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"

	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/types"
)

// Sponsoring a PENDING collection completes its endorsement stage: it turns
// ACTIVE and permanent and leaves the unendorsed-collection prune, which used
// to delete it after sponsorship (burning the sponsor fee and the escrow).
// A HIDDEN collection cannot be sponsored.
func TestSponsorshipOfPendingCollection(t *testing.T) {
	t.Run("pending collection is promoted and never pruned", func(t *testing.T) {
		f := initTestFixture(t)
		collID := f.createPendingCollection(t)
		_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{Creator: f.nonMember, CollectionId: collID})
		require.NoError(t, err)
		_, err = f.msgServer.SponsorCollection(f.ctx, &types.MsgSponsorCollection{Creator: f.member, CollectionId: collID})
		require.NoError(t, err)

		coll, err := f.keeper.Collection.Get(f.ctx, collID)
		require.NoError(t, err)
		require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
		require.Equal(t, int64(0), coll.ExpiresAt)
		require.False(t, coll.Immutable, "no endorser stake stands behind the content")
		require.Zero(t, coll.EndorsementPendingUntil)
		n := 0
		require.NoError(t, f.keeper.EndorsementPending.Walk(f.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			if key.K2() == collID {
				n++
			}
			return false, nil
		}))
		require.Zero(t, n, "out of the unendorsed-collection prune")
	})
	t.Run("hidden collection is refused", func(t *testing.T) {
		f := initTestFixture(t)
		collID := f.createEndorsedCollection(t)
		_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{Creator: f.nonMember, CollectionId: collID})
		require.NoError(t, err)
		coll, err := f.keeper.Collection.Get(f.ctx, collID)
		require.NoError(t, err)
		coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
		require.NoError(t, f.keeper.Collection.Set(f.ctx, collID, coll))
		_, err = f.msgServer.SponsorCollection(f.ctx, &types.MsgSponsorCollection{Creator: f.member, CollectionId: collID})
		require.ErrorIs(t, err, types.ErrCollectionNotActive)
	})
}

// A sentinel cannot hide its own collection or an item it added.
func TestSentinelCannotHideOwnContent(t *testing.T) {
	hide := func(f *testFixture, targetType types.FlagTargetType, id uint64) error {
		_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
			Creator: f.sentinel, TargetId: id, TargetType: targetType,
			ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
		})
		return err
	}
	t.Run("own collection", func(t *testing.T) {
		f := initTestFixture(t)
		denyCouncil(f)
		collID := f.createCollection(t, f.sentinel)
		require.ErrorIs(t, hide(f, types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID), types.ErrSelfModeration)
	})
	t.Run("item it added to another collection", func(t *testing.T) {
		f := initTestFixture(t)
		denyCouncil(f)
		collID := f.createCollection(t, f.owner)
		f.addCollaborator(t, collID, f.owner, f.sentinel, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
		itemID := f.addItem(t, collID, f.sentinel)
		require.ErrorIs(t, hide(f, types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, itemID), types.ErrSelfModeration)
	})
	t.Run("someone else's collection is allowed", func(t *testing.T) {
		f := initTestFixture(t)
		denyCouncil(f)
		collID := f.createCollection(t, f.owner)
		require.NoError(t, hide(f, types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID))
	})
}

// Collaborator roles must be EDITOR or ADMIN.
func TestCollaboratorRoleValidated(t *testing.T) {
	for _, role := range []types.CollaboratorRole{types.CollaboratorRole_COLLABORATOR_ROLE_UNSPECIFIED, types.CollaboratorRole(7)} {
		f := initTestFixture(t)
		collID := f.createCollection(t, f.owner)
		_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
			Creator: f.owner, CollectionId: collID, Address: f.member, Role: role,
		})
		require.ErrorIs(t, err, types.ErrInvalidCollaboratorRole, "add %s", role)

		f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
		_, err = f.msgServer.UpdateCollaboratorRole(f.ctx, &types.MsgUpdateCollaboratorRole{
			Creator: f.owner, CollectionId: collID, Address: f.member, Role: role,
		})
		require.ErrorIs(t, err, types.ErrInvalidCollaboratorRole, "update %s", role)
	}
}

// A collaborator who is not a member cannot reorder items (spec §5.9), as
// for add, update and remove.
func TestReorderRequiresMemberCollaborator(t *testing.T) {
	f := initTestFixture(t)
	collID := f.createCollection(t, f.owner)
	f.addItem(t, collID, f.owner)
	itemID := f.addItem(t, collID, f.owner)
	f.addCollaborator(t, collID, f.owner, f.nonMember, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)

	_, err := f.msgServer.ReorderItem(f.ctx, &types.MsgReorderItem{Creator: f.nonMember, Id: itemID, NewPosition: 0})
	require.ErrorIs(t, err, types.ErrNotMember)
	_, err = f.msgServer.ReorderItem(f.ctx, &types.MsgReorderItem{Creator: f.owner, Id: itemID, NewPosition: 0})
	require.NoError(t, err)
}

// MakeCollectionPermanent supersedes a pending sponsorship request, as
// MsgUpdateCollection's TTL-to-permanent path does: the request is deleted
// (its escrow refunded) instead of lingering until expiry.
func TestMakePermanentSupersedesSponsorshipRequest(t *testing.T) {
	f := initTestFixture(t)
	collID := f.createEndorsedCollection(t)
	_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{Creator: f.nonMember, CollectionId: collID})
	require.NoError(t, err)
	req, err := f.keeper.SponsorshipRequest.Get(f.ctx, collID)
	require.NoError(t, err)

	_, err = f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{Creator: f.owner, CollectionId: collID})
	require.NoError(t, err)

	_, err = f.keeper.SponsorshipRequest.Get(f.ctx, collID)
	require.Error(t, err, "request deleted")
	has, err := f.keeper.SponsorshipRequestsByExpiry.Has(f.ctx, collections.Join(req.ExpiresAt, collID))
	require.NoError(t, err)
	require.False(t, has, "expiry index entry deleted")
}
