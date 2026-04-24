package keeper_test

import (
	"crypto/sha256"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

func TestEndBlockerPruneExpiredContent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "expire-peer")
	opStr := registerTestBridge(t, f, ms, "expire-peer", "expire-op")

	hash := sha256.Sum256([]byte("expire me"))
	contentID := submitTestContent(t, f, ms, opStr, "expire-peer", hash[:])

	// Manually set content to expire in the past
	content, _ := f.keeper.Content.Get(f.ctx, contentID)
	content.ExpiresAt = 1 // very old
	require.NoError(t, f.keeper.Content.Set(f.ctx, contentID, content))
	require.NoError(t, f.keeper.ContentExpiration.Set(f.ctx, collections.Join(int64(1), contentID)))

	// Run EndBlocker
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	sdkCtx = sdkCtx.WithBlockTime(time.Now())
	require.NoError(t, f.keeper.EndBlocker(sdkCtx))

	// Content should be gone
	_, err := f.keeper.Content.Get(sdkCtx, contentID)
	require.Error(t, err)
}

func TestEndBlockerExpireUnverifiedContent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "unverif-peer")
	opStr := registerTestBridge(t, f, ms, "unverif-peer", "unverif-op")

	hash := sha256.Sum256([]byte("unverified"))
	contentID := submitTestContent(t, f, ms, opStr, "unverif-peer", hash[:])

	// Set verification window to expire in the past
	require.NoError(t, f.keeper.VerificationWindow.Remove(f.ctx, collections.Join(int64(0), contentID)))
	require.NoError(t, f.keeper.VerificationWindow.Set(f.ctx, collections.Join(int64(1), contentID)))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	sdkCtx = sdkCtx.WithBlockTime(time.Now())
	require.NoError(t, f.keeper.EndBlocker(sdkCtx))

	content, err := f.keeper.Content.Get(sdkCtx, contentID)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN, content.Status)
}

func TestEndBlockerReleaseVerifierBond(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "bond-rel-peer")
	opStr := registerTestBridge(t, f, ms, "bond-rel-peer", "bond-rel-op")

	hash := sha256.Sum256([]byte("bond release"))
	contentID := submitTestContent(t, f, ms, opStr, "bond-rel-peer", hash[:])

	verifierStr := bondTestVerifier(t, f, ms, "bond-rel-verif")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	// Verifier should have committed bond on the BondedRole (via mock rep keeper).
	br, err := f.repKeeper.GetBondedRole(f.ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr)
	require.NoError(t, err)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, committed.IsPositive())

	// Set challenge window to expire
	record, _ := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.NoError(t, f.keeper.ChallengeWindow.Remove(f.ctx, collections.Join(record.ChallengeWindowEnds, contentID)))
	require.NoError(t, f.keeper.ChallengeWindow.Set(f.ctx, collections.Join(int64(1), contentID)))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	sdkCtx = sdkCtx.WithBlockTime(time.Now())
	require.NoError(t, f.keeper.EndBlocker(sdkCtx))

	// Bond should be released, unchallenged counter bumped on per-module
	// VerifierActivity.
	br, err = f.repKeeper.GetBondedRole(sdkCtx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr)
	require.NoError(t, err)
	committed, _ = math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, committed.IsZero())

	activity, _ := f.keeper.VerifierActivity.Get(sdkCtx, verifierStr)
	require.Equal(t, uint64(1), activity.UnchallengedVerifications)
}
