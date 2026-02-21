package keeper_test

import (
	"context"
	"fmt"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// registerTleValidator adds a TleValidatorShare record for the given address.
func registerTleValidator(t *testing.T, f *testFixture, addr string) {
	t.Helper()
	require.NoError(t, f.keeper.TleValidatorShare.Set(f.ctx, addr, types.TleValidatorShare{
		Validator:      addr,
		PublicKeyShare: []byte("dummy-share"),
		ShareIndex:     1,
	}))
}

// submitShare stores a decryption share for the given validator+epoch.
func submitShare(t *testing.T, f *testFixture, validator string, epoch uint64) {
	t.Helper()
	key := keeper.TleShareKeyForTest(validator, epoch)
	require.NoError(t, f.keeper.TleDecryptionShare.Set(f.ctx, key, types.TleDecryptionShare{
		Index:     key,
		Validator: validator,
		Epoch:     epoch,
		Share:     []byte("dummy-decryption-share"),
	}))
}

// enableTLE sets TLE-related params on the fixture and returns them.
func enableTLE(t *testing.T, f *testFixture) types.Params {
	t.Helper()
	params := types.DefaultParams()
	params.TleEnabled = true
	// defaults: TleMissWindow=100, TleMissTolerance=10, TleJailEnabled=false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	return params
}

// countEvents returns how many events of the given type are in the SDK context.
func countEvents(f *testFixture, eventType string) int {
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	n := 0
	for _, ev := range sdkCtx.EventManager().Events() {
		if ev.Type == eventType {
			n++
		}
	}
	return n
}

// findEventAttr returns the value of the named attribute in the first matching event.
func findEventAttr(f *testFixture, eventType, attrKey string) (string, bool) {
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	for _, ev := range sdkCtx.EventManager().Events() {
		if ev.Type == eventType {
			for _, attr := range ev.Attributes {
				if attr.Key == attrKey {
					return attr.Value, true
				}
			}
		}
	}
	return "", false
}

// resetEvents clears the event manager on the fixture context.
func resetEvents(f *testFixture) {
	f.sdkCtx = f.sdkCtx.WithEventManager(sdk.NewEventManager())
	f.ctx = f.sdkCtx
}

// secondValidator returns a deterministic second validator address string.
func secondValidator(f *testFixture) string {
	addr := sdk.AccAddress([]byte("validator2__________"))
	s, _ := f.addressCodec.BytesToString(addr)
	return s
}

// =========================================================================
// Phase 1: Liveness Tracking (Observability)
// =========================================================================

func TestTrackTleLiveness_TleDisabled(t *testing.T) {
	f := initTestFixture(t)

	// TLE disabled (default) → no-op.
	require.NoError(t, f.keeper.TrackTleLivenessForTest(f.ctx))

	// No participation records should exist.
	has, err := f.keeper.TleEpochParticipation.Has(f.ctx, 9)
	require.NoError(t, err)
	require.False(t, has)
}

func TestTrackTleLiveness_NoCompletedEpoch(t *testing.T) {
	f := initTestFixture(t)
	enableTLE(t, f)

	// Epoch <= 0 → no-op.
	f.seasonKeeper.getCurrentEpochFn = func(_ context.Context) int64 { return 0 }
	require.NoError(t, f.keeper.TrackTleLivenessForTest(f.ctx))

	has, err := f.keeper.TleEpochParticipation.Has(f.ctx, 0)
	require.NoError(t, err)
	require.False(t, has)
}

func TestTrackTleLiveness_Idempotent(t *testing.T) {
	f := initTestFixture(t)
	enableTLE(t, f)

	f.seasonKeeper.getCurrentEpochFn = func(_ context.Context) int64 { return 5 }

	// Pre-store a participation record for epoch 4 (prevEpoch).
	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 4, types.TleEpochParticipation{
		Epoch:           4,
		RegisteredCount: 1,
		SubmittedCount:  1,
	}))

	registerTleValidator(t, f, f.validator)

	// Should skip because epoch 4 is already recorded.
	resetEvents(f)
	require.NoError(t, f.keeper.TrackTleLivenessForTest(f.ctx))

	// No new events emitted.
	require.Equal(t, 0, countEvents(f, types.EventTLEEpochParticipation))
}

func TestRecordEpochParticipation_NoValidators(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)

	// No registered validators → returns nil, no record stored.
	require.NoError(t, f.keeper.RecordEpochParticipationForTest(f.ctx, 5, params))

	has, err := f.keeper.TleEpochParticipation.Has(f.ctx, 5)
	require.NoError(t, err)
	require.False(t, has)
}

func TestRecordEpochParticipation_AllSubmitted(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)

	val2 := secondValidator(f)
	registerTleValidator(t, f, f.validator)
	registerTleValidator(t, f, val2)

	// Both validators submitted shares for epoch 5.
	submitShare(t, f, f.validator, 5)
	submitShare(t, f, val2, 5)

	f.setBlockHeight(100)
	resetEvents(f)
	require.NoError(t, f.keeper.RecordEpochParticipationForTest(f.ctx, 5, params))

	record, err := f.keeper.TleEpochParticipation.Get(f.ctx, 5)
	require.NoError(t, err)
	require.Equal(t, uint32(2), record.RegisteredCount)
	require.Equal(t, uint32(2), record.SubmittedCount)
	require.Empty(t, record.MissedValidators)
	require.Equal(t, int64(100), record.CheckedAt)

	// Epoch summary event emitted, no miss events.
	require.Equal(t, 1, countEvents(f, types.EventTLEEpochParticipation))
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorMissed))
}

func TestRecordEpochParticipation_SomeMissed(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)

	val2 := secondValidator(f)
	registerTleValidator(t, f, f.validator)
	registerTleValidator(t, f, val2)

	// Only validator 1 submitted; val2 missed.
	submitShare(t, f, f.validator, 3)

	f.setBlockHeight(50)
	resetEvents(f)
	require.NoError(t, f.keeper.RecordEpochParticipationForTest(f.ctx, 3, params))

	record, err := f.keeper.TleEpochParticipation.Get(f.ctx, 3)
	require.NoError(t, err)
	require.Equal(t, uint32(2), record.RegisteredCount)
	require.Equal(t, uint32(1), record.SubmittedCount)
	require.Equal(t, []string{val2}, record.MissedValidators)

	// One miss event for val2, one epoch summary event.
	require.Equal(t, 1, countEvents(f, types.EventTLEValidatorMissed))
	require.Equal(t, 1, countEvents(f, types.EventTLEEpochParticipation))

	missedVal, ok := findEventAttr(f, types.EventTLEValidatorMissed, types.AttributeValidator)
	require.True(t, ok)
	require.Equal(t, val2, missedVal)
}

func TestRecordEpochParticipation_AllMissed(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)

	val2 := secondValidator(f)
	registerTleValidator(t, f, f.validator)
	registerTleValidator(t, f, val2)

	// Neither submitted shares.
	f.setBlockHeight(60)
	resetEvents(f)
	require.NoError(t, f.keeper.RecordEpochParticipationForTest(f.ctx, 7, params))

	record, err := f.keeper.TleEpochParticipation.Get(f.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, uint32(2), record.RegisteredCount)
	require.Equal(t, uint32(0), record.SubmittedCount)
	require.Len(t, record.MissedValidators, 2)

	// Two miss events.
	require.Equal(t, 2, countEvents(f, types.EventTLEValidatorMissed))
}

// ---------------------------------------------------------------------------
// Pruning
// ---------------------------------------------------------------------------

func TestPruneTleParticipation_RemovesOldRecords(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleMissWindow = 5

	// Store participation records for epochs 1-10.
	for i := uint64(1); i <= 10; i++ {
		require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, i, types.TleEpochParticipation{
			Epoch: i,
		}))
	}

	// Prune with currentEpoch=10, window=5 → remove epochs < 5 (i.e. 1,2,3,4).
	require.NoError(t, f.keeper.PruneTleParticipationForTest(f.ctx, 10, params))

	for i := uint64(1); i <= 4; i++ {
		has, err := f.keeper.TleEpochParticipation.Has(f.ctx, i)
		require.NoError(t, err)
		require.False(t, has, "epoch %d should be pruned", i)
	}
	for i := uint64(5); i <= 10; i++ {
		has, err := f.keeper.TleEpochParticipation.Has(f.ctx, i)
		require.NoError(t, err)
		require.True(t, has, "epoch %d should be kept", i)
	}
}

func TestPruneTleParticipation_NothingToPrune(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleMissWindow = 100

	// Store 3 records — all within window.
	for i := uint64(1); i <= 3; i++ {
		require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, i, types.TleEpochParticipation{
			Epoch: i,
		}))
	}

	// currentEpoch=3, window=100 → 3 <= 100, nothing to prune.
	require.NoError(t, f.keeper.PruneTleParticipationForTest(f.ctx, 3, params))

	for i := uint64(1); i <= 3; i++ {
		has, err := f.keeper.TleEpochParticipation.Has(f.ctx, i)
		require.NoError(t, err)
		require.True(t, has)
	}
}

// =========================================================================
// Phase 2: Validator Liveness Flags
// =========================================================================

func TestUpdateLivenessFlags_NewValidator_Active(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)

	// Store one participation record where validator did NOT miss.
	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 1, types.TleEpochParticipation{
		Epoch:            1,
		MissedValidators: []string{},
	}))

	f.setBlockHeight(50)
	resetEvents(f)
	require.NoError(t, f.keeper.UpdateValidatorLivenessFlagsForTest(f.ctx, []string{f.validator}, params))

	record, err := f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.True(t, record.TleActive)
	require.Equal(t, uint32(0), record.MissedCount)
	require.Equal(t, uint32(1), record.WindowSize)
	require.Equal(t, int64(0), record.FlaggedAt)
	require.Equal(t, int64(0), record.RecoveredAt)

	// No transition events.
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorFlaggedInactive))
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorRecovered))
}

func TestUpdateLivenessFlags_ActiveToInactive(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleMissTolerance = 2

	// Existing record: validator is active.
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, f.validator, types.TleValidatorLiveness{
		Validator: f.validator,
		TleActive: true,
	}))

	// Store 3 epoch participation records where the validator missed all 3.
	for i := uint64(1); i <= 3; i++ {
		require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, i, types.TleEpochParticipation{
			Epoch:            i,
			MissedValidators: []string{f.validator},
		}))
	}

	f.setBlockHeight(200)
	resetEvents(f)
	require.NoError(t, f.keeper.UpdateValidatorLivenessFlagsForTest(f.ctx, []string{f.validator}, params))

	record, err := f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.False(t, record.TleActive, "should be inactive after exceeding tolerance")
	require.Equal(t, uint32(3), record.MissedCount)
	require.Equal(t, int64(200), record.FlaggedAt)

	// Flagged inactive event emitted.
	require.Equal(t, 1, countEvents(f, types.EventTLEValidatorFlaggedInactive))
	val, ok := findEventAttr(f, types.EventTLEValidatorFlaggedInactive, types.AttributeValidator)
	require.True(t, ok)
	require.Equal(t, f.validator, val)
}

func TestUpdateLivenessFlags_InactiveToActive(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleMissTolerance = 5

	// Existing record: validator is inactive, flagged at block 100.
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, f.validator, types.TleValidatorLiveness{
		Validator: f.validator,
		TleActive: false,
		FlaggedAt: 100,
	}))

	// Store 2 epoch records with 1 miss (within tolerance of 5).
	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 10, types.TleEpochParticipation{
		Epoch:            10,
		MissedValidators: []string{f.validator},
	}))
	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 11, types.TleEpochParticipation{
		Epoch:            11,
		MissedValidators: []string{},
	}))

	f.setBlockHeight(300)
	resetEvents(f)
	require.NoError(t, f.keeper.UpdateValidatorLivenessFlagsForTest(f.ctx, []string{f.validator}, params))

	record, err := f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.True(t, record.TleActive, "should recover to active")
	require.Equal(t, uint32(1), record.MissedCount)
	require.Equal(t, int64(100), record.FlaggedAt, "flaggedAt preserved from prior flagging")
	require.Equal(t, int64(300), record.RecoveredAt)

	// Recovery event emitted.
	require.Equal(t, 1, countEvents(f, types.EventTLEValidatorRecovered))
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorFlaggedInactive))
}

func TestUpdateLivenessFlags_StaysActive(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleMissTolerance = 10

	// Already active.
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, f.validator, types.TleValidatorLiveness{
		Validator: f.validator,
		TleActive: true,
	}))

	// 0 misses.
	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 1, types.TleEpochParticipation{
		Epoch:            1,
		MissedValidators: []string{},
	}))

	f.setBlockHeight(50)
	resetEvents(f)
	require.NoError(t, f.keeper.UpdateValidatorLivenessFlagsForTest(f.ctx, []string{f.validator}, params))

	record, err := f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.True(t, record.TleActive)

	// No transition events.
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorFlaggedInactive))
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorRecovered))
}

func TestUpdateLivenessFlags_StaysInactive(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleMissTolerance = 1

	// Already inactive.
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, f.validator, types.TleValidatorLiveness{
		Validator:   f.validator,
		TleActive:   false,
		FlaggedAt:   50,
		MissedCount: 5,
	}))

	// Still exceeds tolerance (2 misses > 1).
	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 1, types.TleEpochParticipation{
		Epoch:            1,
		MissedValidators: []string{f.validator},
	}))
	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 2, types.TleEpochParticipation{
		Epoch:            2,
		MissedValidators: []string{f.validator},
	}))

	f.setBlockHeight(400)
	resetEvents(f)
	require.NoError(t, f.keeper.UpdateValidatorLivenessFlagsForTest(f.ctx, []string{f.validator}, params))

	record, err := f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.False(t, record.TleActive)
	require.Equal(t, int64(50), record.FlaggedAt, "flaggedAt should not change")

	// No transition events.
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorFlaggedInactive))
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorRecovered))
}

// =========================================================================
// Phase 3: Jailing
// =========================================================================

func TestJailing_JailsOnTransition(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleJailEnabled = true
	params.TleMissTolerance = 0
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Configure staking keeper: return unjailed bonded validator with a consensus key.
	privKey := ed25519.GenPrivKey()
	pkAny, err := codectypes.NewAnyWithValue(privKey.PubKey())
	require.NoError(t, err)
	f.stakingKeeper.getValidatorFn = func(_ context.Context, _ sdk.ValAddress) (stakingtypes.Validator, error) {
		return stakingtypes.Validator{
			Status:          stakingtypes.Bonded,
			Jailed:          false,
			ConsensusPubkey: pkAny,
		}, nil
	}

	// Mark validator as previously active.
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, f.validator, types.TleValidatorLiveness{
		Validator: f.validator,
		TleActive: true,
	}))

	registerTleValidator(t, f, f.validator)

	// Validator missed epoch 1 (exceeds tolerance of 0).
	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 1, types.TleEpochParticipation{
		Epoch:            1,
		MissedValidators: []string{f.validator},
	}))

	f.seasonKeeper.getCurrentEpochFn = func(_ context.Context) int64 { return 3 }
	// Submit share for epoch 2 so recordEpochParticipation runs for prevEpoch=2.
	submitShare(t, f, f.validator, 2)

	f.setBlockHeight(500)
	resetEvents(f)

	// ProcessEndBlock triggers trackTleLiveness → recordEpochParticipation → updateValidatorLivenessFlags.
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	// Validator should be flagged inactive (missed 1 > tolerance 0).
	record, err := f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.False(t, record.TleActive)

	// Both flagged-inactive and jailed events should be emitted.
	require.Equal(t, 1, countEvents(f, types.EventTLEValidatorFlaggedInactive))
	require.Equal(t, 1, countEvents(f, types.EventTLEValidatorJailed))
	require.Len(t, f.stakingKeeper.jailCalls, 1)
}

func TestJailing_SkipsWhenDisabled(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleJailEnabled = false // explicitly disabled
	params.TleMissTolerance = 0

	// Validator was active, now exceeds tolerance.
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, f.validator, types.TleValidatorLiveness{
		Validator: f.validator,
		TleActive: true,
	}))

	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 1, types.TleEpochParticipation{
		Epoch:            1,
		MissedValidators: []string{f.validator},
	}))

	f.setBlockHeight(100)
	resetEvents(f)
	require.NoError(t, f.keeper.UpdateValidatorLivenessFlagsForTest(f.ctx, []string{f.validator}, params))

	// Flagged inactive, but no jail.
	require.Equal(t, 1, countEvents(f, types.EventTLEValidatorFlaggedInactive))
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorJailed))
	require.Len(t, f.stakingKeeper.jailCalls, 0)
}

func TestJailing_SkipsAlreadyJailed(t *testing.T) {
	f := initTestFixture(t)

	// Return a validator that is already jailed.
	f.stakingKeeper.getValidatorFn = func(_ context.Context, _ sdk.ValAddress) (stakingtypes.Validator, error) {
		return stakingtypes.Validator{
			Status: stakingtypes.Bonded,
			Jailed: true,
		}, nil
	}

	f.setBlockHeight(100)
	err := f.keeper.JailTleValidatorForTest(f.ctx, f.validator, 5)
	require.NoError(t, err)

	// Jail was NOT called (skipped due to already jailed).
	require.Len(t, f.stakingKeeper.jailCalls, 0)
	require.Equal(t, 0, countEvents(f, types.EventTLEValidatorJailed))
}

func TestJailing_ErrorDoesNotPropagate(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleJailEnabled = true
	params.TleMissTolerance = 0

	// Make GetValidator return an error to simulate jailing failure.
	f.stakingKeeper.getValidatorFn = func(_ context.Context, _ sdk.ValAddress) (stakingtypes.Validator, error) {
		return stakingtypes.Validator{}, fmt.Errorf("staking module unavailable")
	}

	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, f.validator, types.TleValidatorLiveness{
		Validator: f.validator,
		TleActive: true,
	}))

	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 1, types.TleEpochParticipation{
		Epoch:            1,
		MissedValidators: []string{f.validator},
	}))

	f.setBlockHeight(100)
	resetEvents(f)

	// updateValidatorLivenessFlags should NOT return error — jail failure is logged.
	require.NoError(t, f.keeper.UpdateValidatorLivenessFlagsForTest(f.ctx, []string{f.validator}, params))

	// Validator is still flagged inactive despite jail failure.
	record, err := f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.False(t, record.TleActive)
	require.Equal(t, 1, countEvents(f, types.EventTLEValidatorFlaggedInactive))
}

// =========================================================================
// Integration: Full EndBlock flow
// =========================================================================

func TestEndBlock_TleLiveness_FullFlow(t *testing.T) {
	f := initTestFixture(t)
	params := enableTLE(t, f)
	params.TleMissTolerance = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	val2 := secondValidator(f)
	registerTleValidator(t, f, f.validator)
	registerTleValidator(t, f, val2)

	// Epoch 5 is current → will track epoch 4.
	f.seasonKeeper.getCurrentEpochFn = func(_ context.Context) int64 { return 5 }

	// Validator 1 submitted for epoch 4; val2 did not.
	submitShare(t, f, f.validator, 4)

	f.setBlockHeight(1000)
	resetEvents(f)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	// Epoch participation recorded.
	record, err := f.keeper.TleEpochParticipation.Get(f.ctx, 4)
	require.NoError(t, err)
	require.Equal(t, uint32(2), record.RegisteredCount)
	require.Equal(t, uint32(1), record.SubmittedCount)
	require.Equal(t, []string{val2}, record.MissedValidators)

	// Liveness records created.
	lv1, err := f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.True(t, lv1.TleActive)
	require.Equal(t, uint32(0), lv1.MissedCount)

	lv2, err := f.keeper.TleValidatorLiveness.Get(f.ctx, val2)
	require.NoError(t, err)
	require.True(t, lv2.TleActive, "1 miss <= tolerance of 1, still active")
	require.Equal(t, uint32(1), lv2.MissedCount)

	// --- Second epoch: val2 misses again ---
	f.seasonKeeper.getCurrentEpochFn = func(_ context.Context) int64 { return 6 }
	submitShare(t, f, f.validator, 5)
	// val2 does NOT submit for epoch 5.

	f.setBlockHeight(1100)
	resetEvents(f)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	// val2 now has 2 misses > tolerance of 1 → flagged inactive.
	lv2, err = f.keeper.TleValidatorLiveness.Get(f.ctx, val2)
	require.NoError(t, err)
	require.False(t, lv2.TleActive)
	require.Equal(t, uint32(2), lv2.MissedCount)
	require.Equal(t, int64(1100), lv2.FlaggedAt)

	// validator 1 stays active.
	lv1, err = f.keeper.TleValidatorLiveness.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.True(t, lv1.TleActive)

	// Flagged inactive event for val2.
	require.Equal(t, 1, countEvents(f, types.EventTLEValidatorFlaggedInactive))
}

// =========================================================================
// Genesis Round-Trip
// =========================================================================

func TestGenesis_TleValidatorLiveness_RoundTrip(t *testing.T) {
	f := initTestFixture(t)

	// Set some liveness records.
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, "val1", types.TleValidatorLiveness{
		Validator:   "val1",
		TleActive:   true,
		MissedCount: 3,
		WindowSize:  10,
		FlaggedAt:   0,
		RecoveredAt: 50,
	}))
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, "val2", types.TleValidatorLiveness{
		Validator:   "val2",
		TleActive:   false,
		MissedCount: 15,
		WindowSize:  10,
		FlaggedAt:   100,
		RecoveredAt: 0,
	}))

	// Export genesis.
	genesis, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, genesis.TleValidatorLivenessMap, 2)

	// Verify exported data.
	livenessMap := make(map[string]types.TleValidatorLiveness)
	for _, lv := range genesis.TleValidatorLivenessMap {
		livenessMap[lv.Validator] = lv
	}
	require.True(t, livenessMap["val1"].TleActive)
	require.Equal(t, uint32(3), livenessMap["val1"].MissedCount)
	require.False(t, livenessMap["val2"].TleActive)
	require.Equal(t, int64(100), livenessMap["val2"].FlaggedAt)

	// Create a fresh keeper and import.
	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *genesis))

	// Verify imported data matches.
	lv1, err := f2.keeper.TleValidatorLiveness.Get(f2.ctx, "val1")
	require.NoError(t, err)
	require.True(t, lv1.TleActive)
	require.Equal(t, uint32(3), lv1.MissedCount)
	require.Equal(t, int64(50), lv1.RecoveredAt)

	lv2, err := f2.keeper.TleValidatorLiveness.Get(f2.ctx, "val2")
	require.NoError(t, err)
	require.False(t, lv2.TleActive)
	require.Equal(t, uint32(15), lv2.MissedCount)
	require.Equal(t, int64(100), lv2.FlaggedAt)
}
