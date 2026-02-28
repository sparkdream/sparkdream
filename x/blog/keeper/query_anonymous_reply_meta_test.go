package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryAnonymousReplyMeta(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)
	queryServer := keeper.NewQueryServerImpl(k)

	tests := []struct {
		name        string
		setup       func()
		req         *types.QueryAnonymousReplyMetaRequest
		expectError bool
		checkResp   func(t *testing.T, resp *types.QueryAnonymousReplyMetaResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "no meta for reply",
			req:  &types.QueryAnonymousReplyMetaRequest{ReplyId: 999},
			checkResp: func(t *testing.T, resp *types.QueryAnonymousReplyMetaResponse) {
				require.Nil(t, resp.Metadata)
			},
		},
		{
			name: "meta exists",
			setup: func() {
				meta := types.AnonymousPostMetadata{
					ContentId:        77,
					Nullifier:        []byte("reply-nullifier"),
					MerkleRoot:       []byte("reply-merkle-root"),
					ProvenTrustLevel: 3,
				}
				k.SetAnonymousReplyMeta(ctx, 5, meta)
			},
			req: &types.QueryAnonymousReplyMetaRequest{ReplyId: 5},
			checkResp: func(t *testing.T, resp *types.QueryAnonymousReplyMetaResponse) {
				require.NotNil(t, resp.Metadata)
				require.Equal(t, uint64(77), resp.Metadata.ContentId)
				require.Equal(t, []byte("reply-nullifier"), resp.Metadata.Nullifier)
				require.Equal(t, []byte("reply-merkle-root"), resp.Metadata.MerkleRoot)
				require.Equal(t, uint32(3), resp.Metadata.ProvenTrustLevel)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			resp, err := queryServer.AnonymousReplyMeta(ctx, tt.req)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
		})
	}
}
