package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

// A federation peer's author-curation list is a collection owned by the
// Operations Committee, whose policy address is not an x/rep member: it
// must still get a member's collection (ACTIVE, permanent), and members it
// adds as collaborators curate it.
func TestCommitteeOwnedCurationCollection(t *testing.T) {
	f := initTestFixture(t)
	committee := f.nonMember // a policy address: not a member
	f.commonsKeeper.isGroupPolicyAddressFn = func(_ context.Context, addr string) bool { return addr == committee }

	collID := f.createCollection(t, committee) // permanent: no TTL
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	require.Equal(t, int64(0), coll.ExpiresAt)

	// the committee (by proposal) adds a member as an editor, who curates
	_, err = f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
		Creator: committee, CollectionId: collID, Address: f.member, Role: types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
	})
	require.NoError(t, err)
	link := func(creator, uri string) uint64 {
		resp, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
			Creator: creator, CollectionId: collID, Title: uri,
			ReferenceType: types.ReferenceType_REFERENCE_TYPE_LINK, Link: &types.LinkReference{Uri: uri},
		})
		require.NoError(t, err)
		return resp.Id
	}
	link(committee, "@alice@phoenix.example")
	hidden := link(f.member, "https://phoenix.example/@bob")
	link(f.member, "@carol@phoenix.example")
	f.addItem(t, collID, f.member) // not a link: ignored

	item, err := f.keeper.Item.Get(f.ctx, hidden)
	require.NoError(t, err)
	item.Status = types.ItemStatus_ITEM_STATUS_HIDDEN
	require.NoError(t, f.keeper.Item.Set(f.ctx, hidden, item))

	uris, ok, err := f.keeper.ActiveLinkURIs(f.ctx, collID)
	require.NoError(t, err)
	require.True(t, ok)
	require.ElementsMatch(t, []string{"@alice@phoenix.example", "@carol@phoenix.example"}, uris)
}

func TestActiveLinkURIsInactiveOrMissing(t *testing.T) {
	f := initTestFixture(t)
	_, ok, err := f.keeper.ActiveLinkURIs(f.ctx, 999)
	require.NoError(t, err)
	require.False(t, ok, "no such collection")

	// a non-member's collection starts PENDING: it curates nobody
	pending := f.createPendingCollection(t)
	_, ok, err = f.keeper.ActiveLinkURIs(f.ctx, pending)
	require.NoError(t, err)
	require.False(t, ok)

	// the policy exception is about ownership only: an ordinary non-member
	// still cannot create a permanent collection
	_, err = f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator: f.nonMember, Type: types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC, Name: "not-a-committee",
	})
	require.ErrorIs(t, err, types.ErrNonMemberPermanent)
}
