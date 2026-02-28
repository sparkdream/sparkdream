package keeper_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

// signManagementPayload signs the canonical management payload for testing.
func signManagementPayload(privKey ed25519.PrivateKey, collID, nonce uint64, action types.AnonymousManageAction) []byte {
	buf := make([]byte, 20)
	binary.BigEndian.PutUint64(buf[0:8], collID)
	binary.BigEndian.PutUint64(buf[8:16], nonce)
	binary.BigEndian.PutUint32(buf[16:20], uint32(action))
	hash := sha256.Sum256(buf)
	return ed25519.Sign(privKey, hash[:])
}

// setupAnonCollection creates an anonymous collection and returns its ID + Ed25519 keys.
func setupAnonCollection(t *testing.T, f *testFixture) (uint64, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	msg := &types.MsgCreateAnonymousCollection{
		Submitter:           f.member,
		Type:                types.CollectionType_COLLECTION_TYPE_MIXED,
		ExpiresAt:           10000,
		Name:                "anon-mgmt",
		ManagementPublicKey: pub,
		Proof:               []byte("proof"),
		Nullifier:           []byte("unique_null_mgmt"),
		MerkleRoot:          []byte("root"),
		MinTrustLevel:       2,
	}

	resp, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)
	return resp.Id, pub, priv
}

func TestManageAnonymousCollection_AddItem(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	sig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)

	resp, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: sig,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "new item"}},
	})
	require.NoError(t, err)
	require.Len(t, resp.ItemIds, 1)

	// Verify item exists
	item, err := f.keeper.Item.Get(f.ctx, resp.ItemIds[0])
	require.NoError(t, err)
	require.Equal(t, "new item", item.Title)
	require.Equal(t, collID, item.CollectionId)

	// Verify collection item count updated
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, uint64(1), coll.ItemCount)
}

func TestManageAnonymousCollection_AddItems(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	sig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS)

	resp, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS,
		ManagementSignature: sig,
		Nonce:               1,
		Items: []types.AddItemEntry{
			{Title: "item1"},
			{Title: "item2"},
			{Title: "item3"},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.ItemIds, 3)

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, uint64(3), coll.ItemCount)
}

func TestManageAnonymousCollection_RemoveItem(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// Add two items: item IDs start at 0, and handleAnonRemoveItem treats
	// TargetItemId==0 as "required field missing". By adding two items,
	// the second item gets ID > 0 which we can then remove.
	addSig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS)
	addResp, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS,
		ManagementSignature: addSig,
		Nonce:               1,
		Items: []types.AddItemEntry{
			{Title: "dummy"},
			{Title: "to-remove"},
		},
	})
	require.NoError(t, err)
	require.Len(t, addResp.ItemIds, 2)
	itemID := addResp.ItemIds[1] // second item, ID > 0

	// Remove the second item
	rmSig := signManagementPayload(priv, collID, 2, types.AnonymousManageAction_ANON_MANAGE_ACTION_REMOVE_ITEM)
	_, err = f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_REMOVE_ITEM,
		ManagementSignature: rmSig,
		Nonce:               2,
		TargetItemId:        itemID,
	})
	require.NoError(t, err)

	// Verify item removed
	_, err = f.keeper.Item.Get(f.ctx, itemID)
	require.Error(t, err)

	// Verify collection item count updated (had 2, removed 1 = 1)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, uint64(1), coll.ItemCount)
}

func TestManageAnonymousCollection_UpdateMetadata(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	sig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_METADATA)
	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:             f.member,
		CollectionId:          collID,
		Action:                types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_METADATA,
		ManagementSignature:   sig,
		Nonce:                 1,
		CollectionName:        "updated-name",
		CollectionDescription: "new desc",
		MetadataTags:          []string{"new-tag"},
	})
	require.NoError(t, err)

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, "updated-name", coll.Name)
	require.Equal(t, "new desc", coll.Description)
	require.Equal(t, []string{"new-tag"}, coll.Tags)
}

func TestManageAnonymousCollection_InvalidSignature(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, _ := setupAnonCollection(t, f)

	// Use wrong signature
	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: []byte("invalid_sig"),
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "item"}},
	})
	require.ErrorIs(t, err, types.ErrInvalidManagementSignature)
}

func TestManageAnonymousCollection_NonceNotIncreasing(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// First call with nonce=1
	sig1 := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)
	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: sig1,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "item"}},
	})
	require.NoError(t, err)

	// Try again with nonce=1 (not increasing)
	sig1Again := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)
	_, err = f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: sig1Again,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "item2"}},
	})
	require.ErrorIs(t, err, types.ErrInvalidNonce)
}

func TestManageAnonymousCollection_NotAnonymous(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Create a regular (non-anonymous) collection
	collID := f.createCollection(t, f.owner)

	_, priv, _ := ed25519.GenerateKey(nil)
	sig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)

	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: sig,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "item"}},
	})
	require.ErrorIs(t, err, types.ErrNotAnonymousCollection)
}

func TestManageAnonymousCollection_CollectionNotFound(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        9999,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: []byte("sig"),
		Nonce:               1,
	})
	require.ErrorIs(t, err, types.ErrCollectionNotFound)
}

func TestManageAnonymousCollection_Expired(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// Advance past expiry
	f.setBlockHeight(20000)

	sig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)
	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: sig,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "item"}},
	})
	require.ErrorIs(t, err, types.ErrCollectionExpired)
}

func TestManageAnonymousCollection_UpdateItem(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// Add an item first
	addSig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)
	addResp, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: addSig,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "original"}},
	})
	require.NoError(t, err)
	itemID := addResp.ItemIds[0]

	// Update item title
	updateSig := signManagementPayload(priv, collID, 2, types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_ITEM)
	_, err = f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_ITEM,
		ManagementSignature: updateSig,
		Nonce:               2,
		TargetItemId:        itemID,
		Title:               "updated-title",
	})
	require.NoError(t, err)

	item, _ := f.keeper.Item.Get(f.ctx, itemID)
	require.Equal(t, "updated-title", item.Title)
}

func TestManageAnonymousCollection_ReorderItem(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// Add two items
	addSig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS)
	addResp, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS,
		ManagementSignature: addSig,
		Nonce:               1,
		Items: []types.AddItemEntry{
			{Title: "first"},
			{Title: "second"},
		},
	})
	require.NoError(t, err)
	require.Len(t, addResp.ItemIds, 2)

	// Reorder first item to position 1
	reorderSig := signManagementPayload(priv, collID, 2, types.AnonymousManageAction_ANON_MANAGE_ACTION_REORDER_ITEM)
	_, err = f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_REORDER_ITEM,
		ManagementSignature: reorderSig,
		Nonce:               2,
		TargetItemId:        addResp.ItemIds[0],
		NewPosition:         1,
	})
	require.NoError(t, err)

	item, _ := f.keeper.Item.Get(f.ctx, addResp.ItemIds[0])
	require.Equal(t, uint64(1), item.Position)
}

func TestManageAnonymousCollection_ReorderInvalidPosition(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// Add one item
	addSig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)
	addResp, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: addSig,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "only"}},
	})
	require.NoError(t, err)

	// Try to reorder to position >= item count (1 item, position 1 is out of range)
	reorderSig := signManagementPayload(priv, collID, 2, types.AnonymousManageAction_ANON_MANAGE_ACTION_REORDER_ITEM)
	_, err = f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_REORDER_ITEM,
		ManagementSignature: reorderSig,
		Nonce:               2,
		TargetItemId:        addResp.ItemIds[0],
		NewPosition:         5, // out of range
	})
	require.ErrorIs(t, err, types.ErrInvalidPosition)
}

func TestManageAnonymousCollection_UpdateMetadataNameTooLong(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	sig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_METADATA)
	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_METADATA,
		ManagementSignature: sig,
		Nonce:               1,
		CollectionName:      strings.Repeat("x", 129), // default max 128
	})
	require.ErrorIs(t, err, types.ErrInvalidName)
}

func TestManageAnonymousCollection_RemoveItems(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// Add items
	addSig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS)
	addResp, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS,
		ManagementSignature: addSig,
		Nonce:               1,
		Items: []types.AddItemEntry{
			{Title: "item1"},
			{Title: "item2"},
		},
	})
	require.NoError(t, err)

	// Remove both items
	rmSig := signManagementPayload(priv, collID, 2, types.AnonymousManageAction_ANON_MANAGE_ACTION_REMOVE_ITEMS)
	_, err = f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_REMOVE_ITEMS,
		ManagementSignature: rmSig,
		Nonce:               2,
		ItemIds:             addResp.ItemIds,
	})
	require.NoError(t, err)

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, uint64(0), coll.ItemCount)
}

func TestManageAnonymousCollection_HiddenNotActive(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// Manually set collection to HIDDEN
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	sig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)
	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: sig,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "item"}},
	})
	require.ErrorIs(t, err, types.ErrNotPublicActive)
}

func TestManageAnonymousCollection_UnknownAction(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// Use action=99 (unknown)
	action := types.AnonymousManageAction(99)
	sig := signManagementPayload(priv, collID, 1, action)
	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              action,
		ManagementSignature: sig,
		Nonce:               1,
	})
	require.Error(t, err)
}

func TestManageAnonymousCollection_MaxItemsPerCollection(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Set max items per collection to 1
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxItemsPerCollection = 1
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	collID, _, priv := setupAnonCollection(t, f)

	// Add one item (should succeed)
	addSig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)
	_, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: addSig,
		Nonce:               1,
		Items:               []types.AddItemEntry{{Title: "item1"}},
	})
	require.NoError(t, err)

	// Add another (should fail)
	addSig2 := signManagementPayload(priv, collID, 2, types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM)
	_, err = f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM,
		ManagementSignature: addSig2,
		Nonce:               2,
		Items:               []types.AddItemEntry{{Title: "item2"}},
	})
	require.ErrorIs(t, err, types.ErrMaxItems)
}

// Verify that initAnonFixture rebuilds msgServer properly (tests that value-copy
// keeper has the voteKeeper set).
func TestInitAnonFixture_RebuildsCorrectly(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// This should work because voteKeeper was wired before NewMsgServerImpl
	msg := validCreateAnonCollMsg(f.member)
	resp, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Also verify we get a proper queryServer
	qResp, err := f.queryServer.IsCollectNullifierUsed(f.ctx, &types.QueryIsCollectNullifierUsedRequest{
		NullifierHex: "abcdef",
		Domain:       6,
		Scope:        0,
	})
	require.NoError(t, err)
	require.NotNil(t, qResp)
}

// Ensure the response types match what we expect
func TestManageAnonymousCollectionResponse_Empty(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID, _, priv := setupAnonCollection(t, f)

	// UpdateMetadata returns empty ItemIds
	sig := signManagementPayload(priv, collID, 1, types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_METADATA)
	resp, err := f.msgServer.ManageAnonymousCollection(f.ctx, &types.MsgManageAnonymousCollection{
		Submitter:           f.member,
		CollectionId:        collID,
		Action:              types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_METADATA,
		ManagementSignature: sig,
		Nonce:               1,
		CollectionName:      "new",
	})
	require.NoError(t, err)
	require.Nil(t, resp.ItemIds)
}

// Ensure bankKeeper.sendCoinsFromAccountToModuleFn is reset properly in initAnonFixture
// by verifying deposit escrow works (default mock returns nil)
func TestCreateAnonymousCollection_DefaultBankMockDeposit(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Verify default base deposit is positive
	params, _ := f.keeper.Params.Get(f.ctx)
	require.True(t, params.BaseCollectionDeposit.GT(math.ZeroInt()))

	msg := validCreateAnonCollMsg(f.member)
	resp, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)

	coll, _ := f.keeper.Collection.Get(f.ctx, resp.Id)
	require.True(t, coll.DepositAmount.GT(math.ZeroInt()))
}
