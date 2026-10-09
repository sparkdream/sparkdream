package keeper_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestFederatedContentMediaLabelsAndWithholding(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "media-peer")
	opStr := registerTestBridge(t, f, ms, "media-peer", "media-op")

	submit := func(remoteID, body, uri string) uint64 {
		hash := sha256.Sum256([]byte(remoteID))
		resp, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
			License:  "CC0-1.0",
			Operator: opStr, PeerId: "media-peer", RemoteContentId: remoteID,
			ContentType: "blog_post", CreatorIdentity: "@aurora@media-peer",
			Title: "t", Body: body, ContentUri: uri, ContentHash: hash[:],
		})
		require.NoError(t, err)
		return resp.ContentId
	}

	plainID := submit("plain", "<p>hello</p>", "")
	inline := `<p><img src="data:image/png;base64,iVBORw0KGgo="></p>`
	inlineID := submit("inline", inline, "")
	linkedID := submit("linked", "<p>linked</p>", "https://media-peer/s/1")

	plain, err := f.keeper.Content.Get(f.ctx, plainID)
	require.NoError(t, err)
	require.Zero(t, plain.MediaFlags)
	require.Equal(t, commontypes.MediaRulesVersion, plain.MediaRulesVersion)

	stored, err := f.keeper.Content.Get(f.ctx, inlineID)
	require.NoError(t, err)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA), stored.MediaFlags)

	linked, err := f.keeper.Content.Get(f.ctx, linkedID)
	require.NoError(t, err)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_EXTERNAL_URI), linked.MediaFlags)

	got, err := qs.GetFederatedContent(f.ctx, &types.QueryGetFederatedContentRequest{Id: inlineID})
	require.NoError(t, err)
	require.Empty(t, got.Content.Body)
	got, err = qs.GetFederatedContent(f.ctx, &types.QueryGetFederatedContentRequest{Id: plainID})
	require.NoError(t, err)
	require.Equal(t, "<p>hello</p>", got.Content.Body)

	// Both the unfiltered and the index-filtered list paths withhold.
	for _, req := range []*types.QueryListFederatedContentRequest{{}, {PeerId: "media-peer"}} {
		list, err := qs.ListFederatedContent(f.ctx, req)
		require.NoError(t, err)
		require.Len(t, list.Content, 3)
		for _, c := range list.Content {
			if c.MediaFlags != 0 {
				require.Empty(t, c.Body, "content %d", c.Id)
			} else {
				require.NotEmpty(t, c.Body, "content %d", c.Id)
			}
		}
	}

	body, err := qs.FederatedContentBody(f.ctx, &types.QueryFederatedContentBodyRequest{Id: inlineID})
	require.NoError(t, err)
	require.Equal(t, inline, body.Body)
	require.Equal(t, stored.MediaFlags, body.MediaFlags)
	require.Equal(t, stored.ContentHash, body.ContentHash)

	_, err = qs.FederatedContentBody(f.ctx, &types.QueryFederatedContentBodyRequest{Id: 999999})
	require.Error(t, err)
}
