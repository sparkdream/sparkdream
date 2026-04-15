package keeper_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestSubmitFederatedContent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "content-peer")
	opStr := registerTestBridge(t, f, ms, "content-peer", "content-op")

	hash := sha256.Sum256([]byte("Hello World"))

	resp, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		Operator: opStr, PeerId: "content-peer", RemoteContentId: "post-1",
		ContentType: "blog_post", CreatorIdentity: "@alice@example.com",
		Title: "Hello", Body: "World", ContentHash: hash[:],
	})
	require.NoError(t, err)

	content, err := f.keeper.Content.Get(f.ctx, resp.ContentId)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION, content.Status)
	require.Equal(t, "content-peer", content.PeerId)
	require.Equal(t, "@alice@example.com", content.CreatorIdentity)
}

func TestSubmitFederatedContentDuplicateHash(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "dup-peer")
	opStr := registerTestBridge(t, f, ms, "dup-peer", "dup-op")

	hash := sha256.Sum256([]byte("duplicate content"))
	submitTestContent(t, f, ms, opStr, "dup-peer", hash[:])

	// Second submission with same hash fails
	_, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		Operator: opStr, PeerId: "dup-peer", RemoteContentId: "post-2",
		ContentType: "blog_post", CreatorIdentity: "@bob@example.com",
		ContentHash: hash[:],
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestSubmitFederatedContentMissingHash(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "nohash-peer")
	opStr := registerTestBridge(t, f, ms, "nohash-peer", "nohash-op")

	_, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		Operator: opStr, PeerId: "nohash-peer", RemoteContentId: "post-3",
		ContentType: "blog_post", CreatorIdentity: "@carol@example.com",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "content hash is required")
}

func TestSubmitFederatedContentWrongType(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "wrongtype-peer")
	opStr := registerTestBridge(t, f, ms, "wrongtype-peer", "wrongtype-op")

	// Peer only allows blog_post and forum_thread inbound
	hash := sha256.Sum256([]byte("wrong type"))
	_, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		Operator: opStr, PeerId: "wrongtype-peer", RemoteContentId: "col-1",
		ContentType: "collection", CreatorIdentity: "@dave@example.com",
		ContentHash: hash[:],
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not allowed")
}
