package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryAnonymousPostMeta(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)
	queryServer := keeper.NewQueryServerImpl(k)

	tests := []struct {
		name        string
		setup       func()
		req         *types.QueryAnonymousPostMetaRequest
		expectError bool
		checkResp   func(t *testing.T, resp *types.QueryAnonymousPostMetaResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "no meta for post",
			req:  &types.QueryAnonymousPostMetaRequest{PostId: 999},
			checkResp: func(t *testing.T, resp *types.QueryAnonymousPostMetaResponse) {
				require.Nil(t, resp.Metadata)
			},
		},
		{
			name: "meta exists",
			setup: func() {
				meta := types.AnonymousPostMetadata{
					ContentId:        42,
					Nullifier:        []byte("test-nullifier"),
					MerkleRoot:       []byte("test-merkle-root"),
					ProvenTrustLevel: 2,
				}
				k.SetAnonymousPostMeta(ctx, 1, meta)
			},
			req: &types.QueryAnonymousPostMetaRequest{PostId: 1},
			checkResp: func(t *testing.T, resp *types.QueryAnonymousPostMetaResponse) {
				require.NotNil(t, resp.Metadata)
				require.Equal(t, uint64(42), resp.Metadata.ContentId)
				require.Equal(t, []byte("test-nullifier"), resp.Metadata.Nullifier)
				require.Equal(t, []byte("test-merkle-root"), resp.Metadata.MerkleRoot)
				require.Equal(t, uint32(2), resp.Metadata.ProvenTrustLevel)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			resp, err := queryServer.AnonymousPostMeta(ctx, tt.req)

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
