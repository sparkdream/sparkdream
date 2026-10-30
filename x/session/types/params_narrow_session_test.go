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
	require.Equal(t, p.NarrowSessionMaxExpiration, p.SessionMaxExpiration(types.NarrowSessionMaxMsgTypes, false))
	require.Equal(t, p.MaxExpiration, p.SessionMaxExpiration(types.NarrowSessionMaxMsgTypes+1, false))
	require.Equal(t, p.MaxExpiration, p.SessionMaxExpiration(1, true))

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
