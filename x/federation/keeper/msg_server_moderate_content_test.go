package keeper_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestModerateContent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "mod-peer")
	opStr := registerTestBridge(t, f, ms, "mod-peer", "mod-op")

	hash := sha256.Sum256([]byte("moderate me"))
	contentID := submitTestContent(t, f, ms, opStr, "mod-peer", hash[:])

	_, err := ms.ModerateContent(f.ctx, &types.MsgModerateContent{
		Authority: f.authority, ContentId: contentID,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN, Reason: "spam",
	})
	require.NoError(t, err)

	content, _ := f.keeper.Content.Get(f.ctx, contentID)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN, content.Status)

	// Reject
	_, err = ms.ModerateContent(f.ctx, &types.MsgModerateContent{
		Authority: f.authority, ContentId: contentID,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED, Reason: "confirmed",
	})
	require.NoError(t, err)
}

func TestModerateContentNotFound(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.ModerateContent(f.ctx, &types.MsgModerateContent{
		Authority: f.authority, ContentId: 999999,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN, Reason: "test",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
