package types_test

import (
	"testing"

	"sparkdream/x/session/types"

	artifacttypes "sparkdream/x/artifact/types"

	"github.com/stretchr/testify/require"
)

// artifactDelegable are the x/artifact messages in the default session
// allowlist: game-server and venue ergonomics, inbox and media triage,
// equipment and lending, reward claims and withdrawals (see the entries'
// comment in params.go and docs/x-artifact-spec.md §7.13). The three mint
// entries (MsgMint, MsgIssue, MsgMintUnits) are delegable only because a
// deposit paid through MsgExecSession records the granter as deposit_payer
// and refunds to it, never to the burner (SES-101): drop them if that ever
// changes.
var artifactDelegable = []string{
	"MsgMint", "MsgUpdateToken", "MsgRedeem", "MsgIssue", "MsgMintUnits",
	"MsgAcceptIncoming", "MsgRejectIncoming", "MsgSetReceivePolicy",
	"MsgProposeMedia", "MsgAcceptMedia", "MsgRejectMedia", "MsgSetDisplayedMedia",
	"MsgNest", "MsgDetach", "MsgEndRental", "MsgClaimEditionRewards",
	"MsgDelist", "MsgDelistUnits", "MsgCancelRentalOffer", "MsgCancelOffer", "MsgCancelOutgoing",
}

// TestArtifactSessionAllowlist pins the delegable x/artifact messages and
// requires every other artifact message to stay out: each one sells, buys,
// prices, bids, transfers, burns, freezes, reveals, renews, pays a fee or
// needs an authority. A new artifact message fails this test until it is
// classified.
func TestArtifactSessionAllowlist(t *testing.T) {
	allowed := make(map[string]bool, len(types.DefaultAllowedMsgTypes))
	for _, m := range types.DefaultAllowedMsgTypes {
		allowed[m] = true
	}
	delegable := map[string]bool{}
	for _, name := range artifactDelegable {
		url := "/sparkdream.artifact.v1." + name
		delegable[url] = true
		require.True(t, allowed[url], "%s must be in DefaultAllowedMsgTypes", url)
		require.False(t, types.NonDelegableSessionMsgs[url], "%s must be delegable", url)
		require.False(t, types.ForbiddenAllowlistMsgs[url], "%s must not be forbidden", url)
	}
	for _, m := range artifacttypes.Msg_serviceDesc.Methods {
		url := "/sparkdream.artifact.v1.Msg" + m.MethodName
		require.Equal(t, delegable[url], allowed[url], "%s: classify it in artifactDelegable or keep it out of DefaultAllowedMsgTypes", url)
	}
	for _, url := range []string{
		"/sparkdream.artifact.v1.MsgCreateClass", "/sparkdream.artifact.v1.MsgCreateEdition",
		"/sparkdream.artifact.v1.MsgTransfer", "/sparkdream.artifact.v1.MsgBuy", "/sparkdream.artifact.v1.MsgBurn",
		"/sparkdream.artifact.v1.MsgForceBuy", "/sparkdream.artifact.v1.MsgBid", "/sparkdream.artifact.v1.MsgMakeOffer",
	} {
		require.False(t, allowed[url], "%s moves or burns value: never delegable", url)
	}
	require.NoError(t, types.DefaultParams().Validate())
}
