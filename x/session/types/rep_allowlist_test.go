package types_test

import (
	"testing"

	"sparkdream/x/session/types"

	reptypes "sparkdream/x/rep/types"

	"github.com/stretchr/testify/require"
)

// TestRepSessionAllowlist pins the delegable x/rep messages (the granter's
// day-to-day initiative work and stake management, types.RepWorkMsgTypes) and
// requires every other x/rep message to stay out: projects, approvals,
// reviews, bounties, transfers, invitations, challenges, jury duty, role
// bonds, tags, reports, appeals and identity. A new x/rep message fails this
// test until it is classified.
func TestRepSessionAllowlist(t *testing.T) {
	allowed := make(map[string]bool, len(types.DefaultAllowedMsgTypes))
	for _, m := range types.DefaultAllowedMsgTypes {
		allowed[m] = true
	}
	delegable := map[string]bool{}
	for _, url := range types.RepWorkMsgTypes {
		delegable[url] = true
		require.True(t, allowed[url], "%s must be in DefaultAllowedMsgTypes", url)
		require.False(t, types.NonDelegableSessionMsgs[url], "%s must be delegable", url)
		require.False(t, types.ForbiddenAllowlistMsgs[url], "%s must not be forbidden", url)
	}
	require.Len(t, delegable, 10, "a new delegable x/rep message needs a rationale in RepWorkMsgTypes")

	methods := map[string]bool{}
	for _, m := range reptypes.Msg_serviceDesc.Methods {
		url := "/sparkdream.rep.v1.Msg" + m.MethodName
		methods[url] = true
		require.Equal(t, delegable[url], allowed[url], "%s: classify it in RepWorkMsgTypes or keep it out of DefaultAllowedMsgTypes", url)
	}
	for url := range delegable {
		require.True(t, methods[url], "%s is not an x/rep message", url)
	}
	for _, url := range []string{
		"/sparkdream.rep.v1.MsgProposeProject", "/sparkdream.rep.v1.MsgCancelProject",
		"/sparkdream.rep.v1.MsgCompleteProject", "/sparkdream.rep.v1.MsgApproveProjectBudget",
		"/sparkdream.rep.v1.MsgApproveInitiative", "/sparkdream.rep.v1.MsgFundInitiativeBounty",
		"/sparkdream.rep.v1.MsgTransferDream", "/sparkdream.rep.v1.MsgBondRole",
		"/sparkdream.rep.v1.MsgCreateChallenge", "/sparkdream.rep.v1.MsgInviteMember",
	} {
		require.True(t, methods[url], "%s is not an x/rep message", url)
		require.False(t, allowed[url], "%s must stay wallet-signed", url)
	}
	require.NoError(t, types.DefaultParams().Validate())
}
