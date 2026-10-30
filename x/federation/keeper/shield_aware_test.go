package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
		"content id 0 with no record must not be shield-compatible")
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 999}),
		"non-existent content must not be shield-compatible")

	// Seed a CHALLENGED content record with an open arbitration round and
	// confirm compatibility flips.
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	seed := func(id uint64) {
		require.NoError(t, f.keeper.Content.Set(f.ctx, id, types.FederatedContent{
			Id:     id,
			Status: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED,
		}))
		require.NoError(t, f.keeper.VerificationRecords.Set(f.ctx, id, types.VerificationRecord{
			ContentId: id, ArbiterResolutionDeadline: now + 3600,
		}))
	}
	seed(42)
	require.True(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 42}),
		"CHALLENGED content must be shield-compatible")

	// FED-33: content id 0 is the first record ContentSeq hands out, not a
	// sentinel.
	seed(0)
	require.True(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 0}),
		"content id 0 must be shield-compatible like any other id")

	// FED-25/26: a closed round is not (escalated, quorum stashed, or past
	// its resolution deadline).
	seed(44)
	require.NoError(t, f.keeper.EscalatedChallenges.Set(f.ctx, 44, types.EscalatedChallenge{ContentId: 44}))
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 44}))
	seed(45)
	require.NoError(t, f.keeper.VerificationRecords.Set(f.ctx, 45, types.VerificationRecord{
		ContentId: 45, ArbiterResolutionDeadline: now + 3600,
		PendingVerifierVerdict: types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT,
	}))
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 45}))
	seed(46)
	require.NoError(t, f.keeper.VerificationRecords.Set(f.ctx, 46, types.VerificationRecord{
		ContentId: 46, ArbiterResolutionDeadline: now - 1,
	}))
	require.False(t, f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{ContentId: 46}))

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
