package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

const testDataURI = "data:image/png;base64,iVBORw0KGgo="

func TestCollectRejectsDataURIs(t *testing.T) {
	f := initTestFixture(t)
	collID := f.createCollection(t, f.owner)

	cases := []struct {
		name string
		msg  *types.MsgAddItem
	}{
		{"image_uri", &types.MsgAddItem{ImageUri: testDataURI}},
		{"link uri", &types.MsgAddItem{ReferenceType: types.ReferenceType_REFERENCE_TYPE_LINK, Link: &types.LinkReference{Uri: testDataURI}}},
		{"nft token_uri", &types.MsgAddItem{ReferenceType: types.ReferenceType_REFERENCE_TYPE_NFT, Nft: &types.NftReference{ChainId: "aurora-1", ContractAddress: "c", TokenId: "1", TokenUri: testDataURI}}},
		{"custom value", &types.MsgAddItem{ReferenceType: types.ReferenceType_REFERENCE_TYPE_CUSTOM, Custom: &types.CustomReference{TypeLabel: "x", Value: testDataURI}}},
		{"custom extra", &types.MsgAddItem{ReferenceType: types.ReferenceType_REFERENCE_TYPE_CUSTOM, Custom: &types.CustomReference{TypeLabel: "x", Value: "v", Extra: []*types.KeyValuePair{{Key: "k", Value: testDataURI}}}}},
		{"attribute value", &types.MsgAddItem{Attributes: []*types.KeyValuePair{{Key: "k", Value: "see data:,inline"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.msg.Creator = f.owner
			tc.msg.CollectionId = collID
			tc.msg.Title = "zenith"
			_, err := f.msgServer.AddItem(f.ctx, tc.msg)
			require.ErrorIs(t, err, types.ErrDataURINotAllowed)
		})
	}

	// Prose that merely mentions "metadata:" is fine.
	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator: f.owner, CollectionId: collID, Title: "zenith",
		Attributes: []*types.KeyValuePair{{Key: "note", Value: "metadata: none, really"}},
	})
	require.NoError(t, err)

	_, err = f.msgServer.AddItems(f.ctx, &types.MsgAddItems{
		Creator: f.owner, CollectionId: collID,
		Items: []types.AddItemEntry{{Title: "ok"}, {Title: "bad", ImageUri: testDataURI}},
	})
	require.ErrorIs(t, err, types.ErrDataURINotAllowed)

	_, err = f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator: f.owner, Type: types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC, Name: "phoenix", CoverUri: testDataURI,
	})
	require.ErrorIs(t, err, types.ErrDataURINotAllowed)

	_, err = f.msgServer.UpdateCollection(f.ctx, &types.MsgUpdateCollection{
		Creator: f.owner, Id: collID, Type: types.CollectionType_COLLECTION_TYPE_MIXED,
		Name: "phoenix", CoverUri: testDataURI,
	})
	require.ErrorIs(t, err, types.ErrDataURINotAllowed)
}

func TestCollectMediaLabels(t *testing.T) {
	f := initTestFixture(t)
	external := uint32(commontypes.MediaFlag_MEDIA_FLAG_EXTERNAL_URI)

	collID := f.createCollection(t, f.owner)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Zero(t, coll.MediaFlags)
	require.Equal(t, commontypes.MediaRulesVersion, coll.MediaRulesVersion)

	_, err = f.msgServer.UpdateCollection(f.ctx, &types.MsgUpdateCollection{
		Creator: f.owner, Id: collID, Type: types.CollectionType_COLLECTION_TYPE_MIXED,
		Name: "phoenix", CoverUri: "https://aurora.example/cover.png",
	})
	require.NoError(t, err)
	coll, _ = f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, external, coll.MediaFlags)

	plain, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{Creator: f.owner, CollectionId: collID, Title: "plain"})
	require.NoError(t, err)
	item, _ := f.keeper.Item.Get(f.ctx, plain.Id)
	require.Zero(t, item.MediaFlags)
	require.Equal(t, commontypes.MediaRulesVersion, item.MediaRulesVersion)

	link, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator: f.owner, CollectionId: collID, Title: "link",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_LINK, Link: &types.LinkReference{Uri: "https://aurora.example/a"},
	})
	require.NoError(t, err)
	item, _ = f.keeper.Item.Get(f.ctx, link.Id)
	require.Equal(t, external, item.MediaFlags)

	batch, err := f.msgServer.AddItems(f.ctx, &types.MsgAddItems{
		Creator: f.owner, CollectionId: collID,
		Items: []types.AddItemEntry{{Title: "img", ImageUri: "ipfs://bafyzenith"}},
	})
	require.NoError(t, err)
	item, _ = f.keeper.Item.Get(f.ctx, batch.Ids[0])
	require.Equal(t, external, item.MediaFlags)

	// Updating an item recomputes its labels.
	_, err = f.msgServer.UpdateItem(f.ctx, &types.MsgUpdateItem{Creator: f.owner, Id: batch.Ids[0], Title: "img"})
	require.NoError(t, err)
	item, _ = f.keeper.Item.Get(f.ctx, batch.Ids[0])
	require.Zero(t, item.MediaFlags)
}

func TestCollectGenesisLabelsLegacyDataURIs(t *testing.T) {
	f := initTestFixture(t)

	gen := types.DefaultGenesis()
	gen.Collections = []types.Collection{{Id: 1, Owner: f.owner, Name: "legacy", CoverUri: testDataURI}}
	gen.Items = []types.Item{{Id: 1, CollectionId: 1, AddedBy: f.owner, Title: "legacy", Attributes: []*types.KeyValuePair{{Key: "k", Value: testDataURI}}}}
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gen))

	inlineExternal := uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA | commontypes.MediaFlag_MEDIA_FLAG_EXTERNAL_URI)
	coll, err := f.keeper.Collection.Get(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, inlineExternal, coll.MediaFlags)
	item, err := f.keeper.Item.Get(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA), item.MediaFlags)
}
