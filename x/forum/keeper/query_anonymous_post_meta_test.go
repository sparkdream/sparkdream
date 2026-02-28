package keeper_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestQueryAnonymousPostMeta_Found(t *testing.T) {
	f := initFixture(t)

	// Store anonymous metadata
	meta := types.AnonymousPostMetadata{
		ContentId:        42,
		Nullifier:        []byte("test-nullifier"),
		MerkleRoot:       []byte("test-root"),
		ProvenTrustLevel: 3,
	}
	f.keeper.SetAnonymousPostMeta(f.ctx, 42, meta)

	// Query it
	queryServer := keeper.NewQueryServerImpl(f.keeper)
	resp, err := queryServer.AnonymousPostMeta(f.ctx, &types.QueryAnonymousPostMetaRequest{PostId: 42})
	require.NoError(t, err)
	require.NotNil(t, resp.Metadata)
	require.Equal(t, uint64(42), resp.Metadata.ContentId)
	require.Equal(t, uint32(3), resp.Metadata.ProvenTrustLevel)
}

func TestQueryAnonymousPostMeta_NotFound(t *testing.T) {
	f := initFixture(t)

	queryServer := keeper.NewQueryServerImpl(f.keeper)
	resp, err := queryServer.AnonymousPostMeta(f.ctx, &types.QueryAnonymousPostMetaRequest{PostId: 999})
	require.NoError(t, err)
	require.Nil(t, resp.Metadata)
}

func TestQueryAnonymousPostMeta_NilRequest(t *testing.T) {
	f := initFixture(t)

	queryServer := keeper.NewQueryServerImpl(f.keeper)
	_, err := queryServer.AnonymousPostMeta(f.ctx, nil)
	require.Error(t, err)
}
