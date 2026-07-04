package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// The author bond and per-tag rep penalty slashed by MsgHideContent must be
// restored by every reversal that favors the author — sentinel self-correct
// (covered in msg_server_unhide_content_test.go), jury overturn, and appeal
// timeout — and by nothing else. These tests pin the parity.

// setupHiddenCollectionWithPenalties creates a tagged collection owned by
// f.owner with a simulated author bond, hides it as f.sentinel, and returns
// (collID, hideRecordID, bondAmount).
func setupHiddenCollectionWithPenalties(t *testing.T, f *testFixture) (uint64, uint64, math.Int) {
	t.Helper()

	createResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "zenith-collection",
		Tags:       []string{"zenith"},
	})
	require.NoError(t, err)
	collID := createResp.Id

	bondAmount := math.NewInt(40_000_000)
	f.repKeeper.getAuthorBondFn = func(_ context.Context, _ reptypes.StakeTargetType, _ uint64) (reptypes.Stake, error) {
		return reptypes.Stake{Amount: bondAmount}, nil
	}
	// Owner has more rep than the penalty, so the full penalty is actually
	// deducted (and therefore restorable).
	f.repKeeper.getReputationScoresFn = func(_ context.Context, _ string) (map[string]string, error) {
		return map[string]string{"zenith": "100.0"}, nil
	}

	hrID := hideCollectionForUnhide(t, f, collID)
	return collID, hrID, bondAmount
}

func requirePenaltiesRestored(t *testing.T, f *testFixture, collID uint64, bondAmount math.Int) {
	t.Helper()
	require.Len(t, f.repKeeper.restoreAuthorBondCalls, 1)
	require.Equal(t, bondAmount, f.repKeeper.restoreAuthorBondCalls[0].amount)
	require.Equal(t, collID, f.repKeeper.restoreAuthorBondCalls[0].targetID)
	require.Equal(t, f.ownerAddr, f.repKeeper.restoreAuthorBondCalls[0].author)

	require.Len(t, f.repKeeper.addReputationCalls, 1)
	require.Equal(t, "zenith", f.repKeeper.addReputationCalls[0].tag)
	require.Equal(t, types.DefaultAuthorRepPenalty, f.repKeeper.addReputationCalls[0].amount)
	require.Equal(t, f.ownerAddr, f.repKeeper.addReputationCalls[0].addr)
}

func appealHide(t *testing.T, f *testFixture, hrID uint64) {
	t.Helper()
	f.advanceBlockHeight(types.DefaultAppealCooldownBlocks + 1)
	_, err := f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
		Creator:      f.owner,
		HideRecordId: hrID,
	})
	require.NoError(t, err)
}

func TestResolveHideAppeal_UpheldRestoresAuthorPenalties(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	collID, hrID, bondAmount := setupHiddenCollectionWithPenalties(t, f)
	appealHide(t, f, hrID)

	require.NoError(t, f.keeper.ResolveHideAppeal(f.ctx, hrID, true))
	requirePenaltiesRestored(t, f, collID, bondAmount)
}

func TestResolveHideAppeal_RejectedDoesNotRestore(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	_, hrID, _ := setupHiddenCollectionWithPenalties(t, f)
	appealHide(t, f, hrID)

	// Sentinel wins — content deleted, penalties stay burned.
	require.NoError(t, f.keeper.ResolveHideAppeal(f.ctx, hrID, false))
	require.Empty(t, f.repKeeper.restoreAuthorBondCalls)
	require.Empty(t, f.repKeeper.addReputationCalls)
}

func TestPruneAppealTimeout_RestoresAuthorPenalties(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	collID, hrID, bondAmount := setupHiddenCollectionWithPenalties(t, f)
	appealHide(t, f, hrID)

	// Jury never resolves — advance past the (re-indexed) appeal deadline.
	hr, err := f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	f.setBlockHeight(hr.AppealDeadline + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Timeout favors the appellant: content restored + penalties restored.
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	requirePenaltiesRestored(t, f, collID, bondAmount)
}

func TestUnhideContent_FlooredDeductionDoesNotMintRep(t *testing.T) {
	// An author with zero rep on the collection's tags loses nothing at hide
	// time (DeductReputation floors at zero). The reversal must therefore
	// restore nothing — restoring the raw penalty param would mint rep from
	// nothing on every hide/unhide cycle.
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	createResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "no-rep-collection",
		Tags:       []string{"zenith"},
	})
	require.NoError(t, err)

	// Default mock: empty score map — actual deduction is zero.
	hrID := hideCollectionForUnhide(t, f, createResp.Id)

	hr, err := f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	require.Equal(t, []string{math.LegacyZeroDec().String()}, hr.RepPenaltyAmounts)

	_, err = f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.sentinel,
		HideRecordId: hrID,
	})
	require.NoError(t, err)
	require.Empty(t, f.repKeeper.addReputationCalls, "floored deduction must not be restored")
}

func TestPruneUnappealedHide_DoesNotRestore(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	_, hrID, _ := setupHiddenCollectionWithPenalties(t, f)

	// No appeal — content is deleted at the deadline, penalties stay burned.
	hr, err := f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	f.setBlockHeight(hr.AppealDeadline + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	hr, err = f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
	require.Empty(t, f.repKeeper.restoreAuthorBondCalls)
	require.Empty(t, f.repKeeper.addReputationCalls)
}
