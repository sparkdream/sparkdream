package types_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sparkdream/x/session/types"
)

func TestParams_NarrowSessionMaxExpiration(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	daemon := []string{"/sparkdream.service.v1.MsgSubmitCheckpoint", "/sparkdream.federation.v1.MsgSubmitFederatedContent"}
	require.Equal(t, p.NarrowSessionMaxExpiration, p.SessionMaxExpiration(daemon, false))
	require.Equal(t, p.NarrowSessionMaxExpiration, p.SessionMaxExpiration(daemon[:1], false))
	require.Equal(t, p.MaxExpiration, p.SessionMaxExpiration(append(daemon, "/sparkdream.federation.v1.MsgVerifyContent"), false), "too many types")
	require.Equal(t, p.MaxExpiration, p.SessionMaxExpiration(daemon[:1], true), "allow_self_revoke")
	require.Equal(t, p.MaxExpiration, p.SessionMaxExpiration(nil, false), "no types")
	// SES-103: narrowness is judged by what the types do, not only how
	// many: a single value-moving or non-daemon type is not narrow.
	require.Equal(t, p.MaxExpiration, p.SessionMaxExpiration([]string{"/sparkdream.artifact.v1.MsgMint"}, false))
	require.Equal(t, p.MaxExpiration, p.SessionMaxExpiration([]string{"/sparkdream.blog.v1.MsgCreatePost"}, false))
	// Every narrow type is in the default ceiling and delegable.
	allowed := map[string]bool{}
	for _, m := range types.DefaultAllowedMsgTypes {
		allowed[m] = true
	}
	for m := range types.NarrowSessionMsgTypes {
		require.True(t, allowed[m], "%s must be in DefaultAllowedMsgTypes", m)
		require.False(t, types.ForbiddenAllowlistMsgs[m], m)
	}

	// The narrow cap may never be shorter than the general one.
	short := p
	short.NarrowSessionMaxExpiration = p.MaxExpiration - time.Second
	require.ErrorContains(t, short.Validate(), "narrow_session_max_expiration")

	// Nor longer than the compiled ceiling.
	long := p
	long.NarrowSessionMaxExpiration = types.MaxParamExpiration + time.Second
	require.ErrorContains(t, long.Validate(), "exceeds ceiling")

	// It round-trips through the operational params.
	op := p.ExtractOperationalParams()
	op.NarrowSessionMaxExpiration = 60 * 24 * time.Hour
	require.Equal(t, op.NarrowSessionMaxExpiration, p.ApplyOperationalParams(op).NarrowSessionMaxExpiration)
}
