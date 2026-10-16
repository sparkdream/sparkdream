package types_test

import (
	"testing"

	"sparkdream/x/session/types"

	"github.com/stretchr/testify/require"
)

// TestArtifactGameMsgsAllowlisted pins the two x/artifact messages a game
// server mints and edits its players' records with (the battle royale's
// career, badge, map and match tokens) in the default session allowlist,
// so the server can run on a host that never holds the game management
// account's key. MsgCreateClass must stay out: it burns SPARK and is
// signed by the account itself, once. See the entries' comment in
// params.go.
func TestArtifactGameMsgsAllowlisted(t *testing.T) {
	allowed := make(map[string]bool, len(types.DefaultAllowedMsgTypes))
	for _, m := range types.DefaultAllowedMsgTypes {
		allowed[m] = true
	}

	for _, m := range []string{
		"/sparkdream.artifact.v1.MsgMint",
		"/sparkdream.artifact.v1.MsgUpdateToken",
	} {
		require.True(t, allowed[m], "%s must be in DefaultAllowedMsgTypes", m)
		require.False(t, types.NonDelegableSessionMsgs[m], "%s must be delegable", m)
		require.False(t, types.ForbiddenAllowlistMsgs[m], "%s must not be forbidden", m)
	}
	require.False(t, allowed["/sparkdream.artifact.v1.MsgCreateClass"], "class creation burns SPARK: never delegable")

	require.NoError(t, types.DefaultParams().Validate())
}
