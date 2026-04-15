package keeper_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestQueryGetFederatedContent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	registerTestPeer(t, f, ms, "content-q-peer")
	opStr := registerTestBridge(t, f, ms, "content-q-peer", "content-q-op")

	hash := sha256.Sum256([]byte("query content"))
	contentID := submitTestContent(t, f, ms, opStr, "content-q-peer", hash[:])

	resp, err := qs.GetFederatedContent(f.ctx, &types.QueryGetFederatedContentRequest{Id: contentID})
	require.NoError(t, err)
	require.Equal(t, contentID, resp.Content.Id)

	_, err = qs.GetFederatedContent(f.ctx, &types.QueryGetFederatedContentRequest{Id: 999999})
	require.Error(t, err)
}
