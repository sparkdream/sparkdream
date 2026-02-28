package keeper_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestQueryAnonymousReplyMeta_Found(t *testing.T) {
	f := initFixture(t)

	meta := types.AnonymousPostMetadata{
		ContentId:        42,
		Nullifier:        []byte("test-nullifier"),
		MerkleRoot:       []byte("test-root"),
		ProvenTrustLevel: 3,
	}
	f.keeper.SetAnonymousReplyMeta(f.ctx, 42, meta)

	queryServer := keeper.NewQueryServerImpl(f.keeper)
	resp, err := queryServer.AnonymousReplyMeta(f.ctx, &types.QueryAnonymousReplyMetaRequest{PostId: 42})
	require.NoError(t, err)
	require.NotNil(t, resp.Metadata)
	require.Equal(t, uint64(42), resp.Metadata.ContentId)
	require.Equal(t, uint32(3), resp.Metadata.ProvenTrustLevel)
}

func TestQueryAnonymousReplyMeta_NotFound(t *testing.T) {
	f := initFixture(t)

	queryServer := keeper.NewQueryServerImpl(f.keeper)
	resp, err := queryServer.AnonymousReplyMeta(f.ctx, &types.QueryAnonymousReplyMetaRequest{PostId: 999})
	require.NoError(t, err)
	require.Nil(t, resp.Metadata)
}

func TestQueryAnonymousReplyMeta_NilRequest(t *testing.T) {
	f := initFixture(t)

	queryServer := keeper.NewQueryServerImpl(f.keeper)
	_, err := queryServer.AnonymousReplyMeta(f.ctx, nil)
	require.Error(t, err)
}
