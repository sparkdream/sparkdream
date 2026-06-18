package types_test

import (
	"testing"

	"sparkdream/x/session/types"

	"github.com/stretchr/testify/require"
)

// TestForumCurationMsgsAllowlisted pins the forum accepted-reply curation
// messages in the default session allowlist. MarkAcceptedReply carries a
// bonded-sentinel privilege path (re-checked at dispatch), so its presence is a
// deliberate exception — this guards against an accidental removal that would
// silently break sentinels' UX-session curation, and against the entries
// drifting out of the delegable set.
func TestForumCurationMsgsAllowlisted(t *testing.T) {
	curationMsgs := []string{
		"/sparkdream.forum.v1.MsgMarkAcceptedReply",
		"/sparkdream.forum.v1.MsgConfirmProposedReply",
		"/sparkdream.forum.v1.MsgRejectProposedReply",
	}

	allowed := make(map[string]bool, len(types.DefaultAllowedMsgTypes))
	for _, m := range types.DefaultAllowedMsgTypes {
		allowed[m] = true
	}

	for _, m := range curationMsgs {
		require.True(t, allowed[m], "%s must be in DefaultAllowedMsgTypes", m)
		require.False(t, types.NonDelegableSessionMsgs[m], "%s must be delegable", m)
	}

	// The default params (ceiling = active = DefaultAllowedMsgTypes) must remain
	// valid with these entries present.
	require.NoError(t, types.DefaultParams().Validate())
}
