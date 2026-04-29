package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/types"
)

func TestShieldAwareCompatibility(t *testing.T) {
	f := initFixture(t)

	// FEDERATION-S2-5: MsgSubmitArbiterHash is only shield-compatible when the
	// referenced content exists and is in CHALLENGED or DISPUTED state. An
	// empty content_id or a missing content record returns false so shield
	// rejects the wrap before the federation handler runs.
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{}),
		"empty content_id must not be shield-compatible")
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 999}),
		"non-existent content must not be shield-compatible")

	// Seed a CHALLENGED content record and confirm compatibility flips.
	require.NoError(t, f.keeper.Content.Set(f.ctx, 42, types.FederatedContent{
		Id:     42,
		Status: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED,
	}))
	require.True(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 42}),
		"CHALLENGED content must be shield-compatible")

	// VERIFIED content is not eligible for arbiter quorum.
	require.NoError(t, f.keeper.Content.Set(f.ctx, 43, types.FederatedContent{
		Id:     43,
		Status: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED,
	}))
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 43}),
		"VERIFIED content must not be shield-compatible")

	// Other federation messages are not shield-compatible regardless of state.
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgRegisterPeer{}))
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgVerifyContent{}))
}
