package keeper_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func validCreateAnonCollMsg(submitter string) *types.MsgCreateAnonymousCollection {
	mgmtKey := make([]byte, 32)
	for i := range mgmtKey {
		mgmtKey[i] = byte(i + 1)
	}
	return &types.MsgCreateAnonymousCollection{
		Submitter:           submitter,
		Type:                types.CollectionType_COLLECTION_TYPE_MIXED,
		ExpiresAt:           1000,
		Name:                "anon-coll",
		Description:         "anon description",
		Tags:                []string{"tag1"},
		ManagementPublicKey: mgmtKey,
		Proof:               []byte("proof"),
		Nullifier:           []byte("nullifier_ac"),
		MerkleRoot:          []byte("root"),
		MinTrustLevel:       2,
	}
}

func TestCreateAnonymousCollection_Success(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	resp, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the collection exists
	coll, err := f.keeper.Collection.Get(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	require.Equal(t, types.Visibility_VISIBILITY_PUBLIC, coll.Visibility)
	require.Equal(t, int64(1000), coll.ExpiresAt)
	require.Equal(t, "anon-coll", coll.Name)
	require.True(t, coll.CommunityFeedbackEnabled)

	// Verify anonymous metadata
	meta, found := f.keeper.GetAnonymousCollectionMeta(f.ctx, resp.Id)
	require.True(t, found)
	require.Equal(t, resp.Id, meta.CollectionId)
	require.Equal(t, msg.ManagementPublicKey, meta.ManagementPublicKey)
	require.Equal(t, uint64(0), meta.Nonce)
	require.Equal(t, uint32(2), meta.ProvenTrustLevel)

	// Verify management key count incremented
	require.Equal(t, uint32(1), f.keeper.GetManagementKeyCollectionCount(f.ctx, msg.ManagementPublicKey))
}

func TestCreateAnonymousCollection_WithInitialItems(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.InitialItems = []types.AddItemEntry{
		{Title: "item1"},
		{Title: "item2"},
	}

	resp, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)
	require.Len(t, resp.ItemIds, 2)

	// Verify collection item count
	coll, _ := f.keeper.Collection.Get(f.ctx, resp.Id)
	require.Equal(t, uint64(2), coll.ItemCount)

	// Verify items exist
	for _, itemID := range resp.ItemIds {
		item, err := f.keeper.Item.Get(f.ctx, itemID)
		require.NoError(t, err)
		require.Equal(t, resp.Id, item.CollectionId)
	}
}

func TestCreateAnonymousCollection_NoVoteKeeper(t *testing.T) {
	f := initAnonFixture(t, nil)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrAnonymousPostingUnavailable)
}

func TestCreateAnonymousCollection_Disabled(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = false
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	msg := validCreateAnonCollMsg(f.member)
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrAnonymousPostingDisabled)
}

func TestCreateAnonymousCollection_InsufficientTrustLevel(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.MinTrustLevel = 1 // below required 2
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrInsufficientAnonTrustLevel)
}

func TestCreateAnonymousCollection_InvalidManagementKey(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.ManagementPublicKey = []byte("too_short") // not 32 bytes
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrInvalidManagementKey)
}

func TestCreateAnonymousCollection_NoTTL(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.ExpiresAt = 0 // anonymous requires TTL
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrAnonymousRequiresTTL)
}

func TestCreateAnonymousCollection_ExpiresAtInPast(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.ExpiresAt = 50 // in the past (block height is 100)
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrInvalidExpiry)
}

func TestCreateAnonymousCollection_ExceedsMaxTTL(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxTtlBlocks = 1000 // set a max TTL
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	msg := validCreateAnonCollMsg(f.member)
	msg.ExpiresAt = 100 + 1001 // exceeds max TTL
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrInvalidExpiry)
}

func TestCreateAnonymousCollection_MaxCollectionsPerKey(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Set max per key to 1
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxAnonymousCollectionsPerKey = 1
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	mgmtKey := make([]byte, 32)
	for i := range mgmtKey {
		mgmtKey[i] = byte(i + 1)
	}

	// Create first collection - should succeed
	msg := validCreateAnonCollMsg(f.member)
	msg.ManagementPublicKey = mgmtKey
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)

	// Create second with same key - should fail
	msg2 := validCreateAnonCollMsg(f.member)
	msg2.ManagementPublicKey = mgmtKey
	msg2.Nullifier = []byte("different_nullifier")
	_, err = f.msgServer.CreateAnonymousCollection(f.ctx, msg2)
	require.ErrorIs(t, err, types.ErrMaxAnonymousCollections)
}

func TestCreateAnonymousCollection_NullifierUsed(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)

	// Same nullifier in same epoch should fail
	msg2 := validCreateAnonCollMsg(f.member)
	// Use a different management key so it doesn't fail on max-per-key
	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 50)
	}
	msg2.ManagementPublicKey = newKey
	// Same nullifier
	msg2.Nullifier = msg.Nullifier
	_, err = f.msgServer.CreateAnonymousCollection(f.ctx, msg2)
	require.ErrorIs(t, err, types.ErrNullifierUsed)
}

func TestCreateAnonymousCollection_InvalidProof(t *testing.T) {
	vk := &mockVoteKeeper{
		verifyFn: func(_ context.Context, _, _, _ []byte, _ uint32) error {
			return fmt.Errorf("bad proof")
		},
	}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrInvalidZKProof)
}

func TestCreateAnonymousCollection_NameValidation(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Empty name
	msg := validCreateAnonCollMsg(f.member)
	msg.Name = ""
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrInvalidName)

	// Name too long
	msg2 := validCreateAnonCollMsg(f.member)
	msg2.Name = strings.Repeat("a", 129) // default max is 128
	msg2.Nullifier = []byte("unique_null_2")
	_, err = f.msgServer.CreateAnonymousCollection(f.ctx, msg2)
	require.ErrorIs(t, err, types.ErrInvalidName)
}

func TestCreateAnonymousCollection_TooManyTags(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.Tags = make([]string, 11) // default max is 10
	for i := range msg.Tags {
		msg.Tags[i] = "tag"
	}
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrMaxTags)
}

func TestCreateAnonymousCollection_TagTooLong(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.Tags = []string{strings.Repeat("x", 33)} // default max tag length is 32
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrTagTooLong)
}

func TestCreateAnonymousCollection_TooManyInitialItems(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.InitialItems = make([]types.AddItemEntry, 51) // default max batch is 50
	for i := range msg.InitialItems {
		msg.InitialItems[i] = types.AddItemEntry{Title: "item"}
	}
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrBatchTooLarge)
}

func TestCreateAnonymousCollection_DepositEscrowed(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	var escrowedAmount sdk.Coins
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, _ string, amt sdk.Coins) error {
		escrowedAmount = escrowedAmount.Add(amt...)
		return nil
	}

	msg := validCreateAnonCollMsg(f.member)
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)

	// Should have escrowed the base collection deposit
	params, _ := f.keeper.Params.Get(f.ctx)
	require.True(t, escrowedAmount.AmountOf("uspark").GTE(params.BaseCollectionDeposit))
}

func TestCreateAnonymousCollection_InsufficientFundsForDeposit(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
		return fmt.Errorf("insufficient funds")
	}

	msg := validCreateAnonCollMsg(f.member)
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrInsufficientFunds)
}

func TestCreateAnonymousCollection_DescriptionTooLong(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	msg := validCreateAnonCollMsg(f.member)
	msg.Description = strings.Repeat("d", 1025) // default max is 1024
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrInvalidDescription)
}

// initAnonFixture is re-used from msg_server_anonymous_react_test.go (same package)
// so we don't redefine it here. The mockVoteKeeper is also shared.
