package types_test

import (
	"testing"

	"sparkdream/x/session/types"

	"github.com/stretchr/testify/require"
)

// TestFederationDaemonMsgsAllowlisted pins the x/federation daemon
// messages in the default session allowlist (Mastodon live link,
// P0.2). The allowlist ceiling is genesis-fixed and only expandable via
// chain upgrade, so dropping one of these entries silently strands the
// bridge daemon / verifier runner on hot keys until the next upgrade —
// exactly what this test exists to catch. MsgVerifyContent is the
// deliberate exception to the no-bond-at-risk rule (it reserves the
// verifier's DREAM slash budget); see its comment in params.go.
func TestFederationDaemonMsgsAllowlisted(t *testing.T) {
	daemonMsgs := []string{
		"/sparkdream.federation.v1.MsgSubmitFederatedContent",
		"/sparkdream.federation.v1.MsgAttestOutbound",
		"/sparkdream.federation.v1.MsgVerifyContent",
		// Content-scanner workers (docs/content-scanning.md §8.2).
		"/sparkdream.service.v1.MsgSubmitCheckpoint",
	}

	allowed := make(map[string]bool, len(types.DefaultAllowedMsgTypes))
	for _, m := range types.DefaultAllowedMsgTypes {
		allowed[m] = true
	}

	for _, m := range daemonMsgs {
		require.True(t, allowed[m], "%s must be in DefaultAllowedMsgTypes", m)
		require.False(t, types.NonDelegableSessionMsgs[m], "%s must be delegable", m)
	}

	// The default params (ceiling = active = DefaultAllowedMsgTypes) must
	// remain valid with these entries present.
	require.NoError(t, types.DefaultParams().Validate())
}
