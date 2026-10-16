package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	artifacttypes "sparkdream/x/artifact/types"
	"sparkdream/x/session/keeper"
)

// The allowlisted x/artifact messages name their signer "minter" and
// "owner": ExecSession must be able to rewrite both to the granter, or a
// session key could mint and edit as itself rather than for the granter.
func TestRewriteSignerFieldArtifact(t *testing.T) {
	mint := &artifacttypes.MsgMint{Minter: "grantee", ClassId: 7}
	require.NoError(t, keeper.RewriteSignerField(mint, "granter"))
	require.Equal(t, "granter", mint.Minter)

	update := &artifacttypes.MsgUpdateToken{Owner: "grantee", ClassId: 7, TokenId: 1}
	require.NoError(t, keeper.RewriteSignerField(update, "granter"))
	require.Equal(t, "granter", update.Owner)
}
