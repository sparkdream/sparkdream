package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

// Council moderation path — see docs/x-collect-spec.md (MsgHideContent /
// MsgUnhideContent).
// f.member stands in for an Operations Committee member (council-authorized,
// not a sentinel); f.sentinel is the bonded sentinel. councilOnly() makes
// exactly the given addresses council-authorized.

func councilOnly(f *testFixture, addrs ...string) {
	allowed := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		allowed[a] = true
	}
	f.commonsKeeper.isCouncilAuthorizedFn = func(_ context.Context, addr string, _, _ string) bool {
		return allowed[addr]
	}
}

func hideWithAuthority(f *testFixture, creator string, collID uint64, authority types.ModerationAuthority) (*types.MsgHideContentResponse, error) {
	return f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    creator,
		TargetId:   collID,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
		ReasonText: "spam content",
		Authority:  authority,
	})
}

func TestModerationAuthority_Resolution(t *testing.T) {
	tests := []struct {
		name           string
		creator        string // "sentinel" | "member" | "owner"
		councilFor     string // which role is council-authorized ("" = nobody)
		authority      types.ModerationAuthority
		expErr         bool
		expErrContains string
		expSentinel    bool // on success: HideRecord.Sentinel == creator (sentinel path)
	}{
		{
			name:        "AUTO dual-role prefers sentinel path",
			creator:     "sentinel",
			councilFor:  "sentinel",
			authority:   types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
			expSentinel: true,
		},
		{
			name:       "AUTO council-only falls through to council path",
			creator:    "member",
			councilFor: "member",
			authority:  types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
		},
		{
			name:           "AUTO neither surfaces sentinel error",
			creator:        "owner",
			authority:      types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
			expErr:         true,
			expErrContains: "not an active forum sentinel",
		},
		{
			name:        "forced COUNCIL by dual-role takes council path",
			creator:     "sentinel",
			councilFor:  "sentinel",
			authority:   types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
			expSentinel: false,
		},
		{
			name:           "forced COUNCIL by non-council rejected",
			creator:        "sentinel",
			authority:      types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
			expErr:         true,
			expErrContains: "not authorized as council",
		},
		{
			name:           "forced SENTINEL by council-only rejected",
			creator:        "member",
			councilFor:     "member",
			authority:      types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL,
			expErr:         true,
			expErrContains: "not an active forum sentinel",
		},
		{
			name:           "out-of-range authority value rejected",
			creator:        "sentinel",
			councilFor:     "sentinel",
			authority:      types.ModerationAuthority(99),
			expErr:         true,
			expErrContains: "invalid moderation authority",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			f.setBlockHeight(100)

			roleAddr := map[string]string{
				"sentinel": f.sentinel,
				"member":   f.member,
				"owner":    f.owner,
			}
			if tc.councilFor != "" {
				councilOnly(f, roleAddr[tc.councilFor])
			} else {
				councilOnly(f) // nobody
			}

			collID := f.createCollection(t, f.owner)
			resp, err := hideWithAuthority(f, roleAddr[tc.creator], collID, tc.authority)

			if tc.expErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expErrContains)
				return
			}
			require.NoError(t, err)

			hr, err := f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
			require.NoError(t, err)
			if tc.expSentinel {
				require.Equal(t, roleAddr[tc.creator], hr.Sentinel, "sentinel path records the sentinel")
				require.True(t, hr.CommittedAmount.IsPositive(), "sentinel path reserves a bond")
			} else {
				require.Equal(t, "", hr.Sentinel, "council path uses the empty-marker convention")
				require.True(t, hr.CommittedAmount.IsZero(), "council path reserves no bond")
			}
		})
	}
}

func TestCouncilHide_NoBondNoRateLimit(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	councilOnly(f, f.member)

	// Tighten the sentinel cap to prove the council path ignores it.
	params := types.DefaultParams()
	params.MaxHidesPerSentinelPerDay = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	committedBefore := sentinelCommitted(f)

	for i := 0; i < 3; i++ {
		collID := f.createCollection(t, f.owner)
		_, err := hideWithAuthority(f, f.member, collID, types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL)
		require.NoError(t, err, "council hide %d must not hit the sentinel daily cap", i+1)
	}

	// No bond movement anywhere (f.member holds no bonded role; the
	// sentinel's committed total is untouched).
	require.Equal(t, committedBefore, sentinelCommitted(f))
}

func TestCouncilHide_AppealableAndJuryOverturnRestores(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	councilOnly(f, f.member)

	// Author penalties apply on council hides too (D5) and must restore on
	// overturn.
	f.repKeeper.getReputationScoresFn = func(_ context.Context, _ string) (map[string]string, error) {
		return map[string]string{"zenith": "100.0"}, nil
	}
	bondAmount := math.NewInt(40_000_000)
	f.repKeeper.getAuthorBondFn = func(_ context.Context, _ reptypes.StakeTargetType, _ uint64) (reptypes.Stake, error) {
		return reptypes.Stake{Amount: bondAmount}, nil
	}

	createResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "council-hide-appeal",
		Tags:       []string{"zenith"},
	})
	require.NoError(t, err)
	collID := createResp.Id

	resp, err := hideWithAuthority(f, f.member, collID, types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL)
	require.NoError(t, err)
	hrID := resp.HideRecordId

	// Council hides ARE appealable (deliberate deviation from forum's
	// unappealable gov hides — collect's lifecycle deletes).
	appealHide(t, f, hrID)

	// Jury overturn: content restored, penalties restored, nothing slashed.
	require.NoError(t, f.keeper.ResolveHideAppeal(f.ctx, hrID, true))
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	requirePenaltiesRestored(t, f, collID, bondAmount)
}

func TestCouncilHide_UnappealedDeletesAtDeadline(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	councilOnly(f, f.member)

	collID := f.createCollection(t, f.owner)
	resp, err := hideWithAuthority(f, f.member, collID, types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL)
	require.NoError(t, err)

	hr, err := f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
	require.NoError(t, err)
	f.setBlockHeight(hr.AppealDeadline + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Same terminal exit as an unappealed sentinel hide: content deleted.
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)
	hr, err = f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
}

func TestCouncilUnhide_OfSentinelHide(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	councilOnly(f, f.member)

	f.repKeeper.getReputationScoresFn = func(_ context.Context, _ string) (map[string]string, error) {
		return map[string]string{"zenith": "100.0"}, nil
	}
	bondAmount := math.NewInt(40_000_000)
	f.repKeeper.getAuthorBondFn = func(_ context.Context, _ reptypes.StakeTargetType, _ uint64) (reptypes.Stake, error) {
		return reptypes.Stake{Amount: bondAmount}, nil
	}

	createResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "council-override",
		Tags:       []string{"zenith"},
	})
	require.NoError(t, err)
	collID := createResp.Id

	committedBase := sentinelCommitted(f)
	hrID := hideCollectionForUnhide(t, f, collID)
	hr, err := f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	deadline := hr.AppealDeadline

	// Council unhide has no window — advance well past the sentinel window.
	f.advanceBlockHeight(types.DefaultSentinelUnhideWindowBlocks + 100)

	_, err = f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.member,
		HideRecordId: hrID,
	})
	require.NoError(t, err)

	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	requirePenaltiesRestored(t, f, collID, bondAmount)

	// Council override (not self-serve): bond released IMMEDIATELY, expiry
	// entry removed, record plain-resolved (not self-corrected).
	hr, err = f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
	require.False(t, hr.SelfCorrected)
	require.Equal(t, committedBase, sentinelCommitted(f))
	hasExpiry, err := f.keeper.HideRecordExpiry.Has(f.ctx, collections.Join(deadline, hrID))
	require.NoError(t, err)
	require.False(t, hasExpiry)
}

func TestCouncilUnhide_OfCouncilHide(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	councilOnly(f, f.member)

	collID := f.createCollection(t, f.owner)
	resp, err := hideWithAuthority(f, f.member, collID, types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL)
	require.NoError(t, err)

	_, err = f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.member,
		HideRecordId: resp.HideRecordId,
	})
	require.NoError(t, err)

	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	hr, err := f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
	require.False(t, hr.SelfCorrected)
}

func TestCouncilUnhide_AppealedRejected(t *testing.T) {
	// Deviation from forum: once appealed, even the council cannot unhide —
	// the jury owns the outcome (the appeal fee is escrowed).
	f := initTestFixture(t)
	f.setBlockHeight(100)
	councilOnly(f, f.member)

	collID := f.createCollection(t, f.owner)
	hrID := hideCollectionForUnhide(t, f, collID)
	appealHide(t, f, hrID)

	_, err := f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.member,
		HideRecordId: hrID,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrHideAppealed)
}
