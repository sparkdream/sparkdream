package ante_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	any "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	shieldante "sparkdream/x/shield/ante"
	shieldtypes "sparkdream/x/shield/types"
)

// guardKeeper is a shield keeper with a stored VK, a registered op and a
// PrecheckImmediate call counter, so tests can see when a proof is verified.
type guardKeeper struct {
	mockShieldKeeper
	vk     []byte
	reg    *shieldtypes.ShieldedOpRegistration
	epoch  uint64
	result error // PrecheckImmediate's result
	calls  int   // PrecheckImmediate calls, i.e. verifications
}

func newGuardKeeper() *guardKeeper {
	return &guardKeeper{
		mockShieldKeeper: mockShieldKeeper{params: shieldtypes.DefaultParams()},
		vk:               []byte("verification-key-v1"),
		reg: &shieldtypes.ShieldedOpRegistration{
			MessageTypeUrl:  "/sparkdream.blog.v1.MsgCreatePost",
			NullifierDomain: 1,
			Active:          true,
		},
		epoch: 7,
	}
}

func (g *guardKeeper) PrecheckImmediate(_ sdk.Context, _ *shieldtypes.MsgShieldedExec) error {
	g.calls++
	return g.result
}

func (g *guardKeeper) GetVerificationKeyVal(_ context.Context, circuitID string) (shieldtypes.VerificationKey, bool) {
	if g.vk == nil || circuitID != shieldante.ShieldCircuitID {
		return shieldtypes.VerificationKey{}, false
	}
	return shieldtypes.VerificationKey{CircuitId: circuitID, VkBytes: g.vk}, true
}

func (g *guardKeeper) GetShieldedOp(_ context.Context, typeURL string) (shieldtypes.ShieldedOpRegistration, bool) {
	if g.reg == nil || typeURL != g.reg.MessageTypeUrl {
		return shieldtypes.ShieldedOpRegistration{}, false
	}
	return *g.reg, true
}

func (g *guardKeeper) GetCurrentEpoch(_ context.Context) uint64 { return g.epoch }

// guardedExec is an immediate exec for guardKeeper's registered op; seed
// varies the nullifier so distinct execs miss the cache.
func guardedExec(seed byte) *shieldtypes.MsgShieldedExec {
	msg := validShieldedExec()
	msg.Nullifier[0] = seed
	msg.InnerMessage = &any.Any{TypeUrl: "/sparkdream.blog.v1.MsgCreatePost", Value: []byte{0x0a, 0x01, 'x'}}
	return msg
}

func checkCtx(t *testing.T) sdk.Context   { return makeTestContext(t).WithIsCheckTx(true) }
func recheckCtx(t *testing.T) sdk.Context { return makeTestContext(t).WithIsReCheckTx(true) }
func deliverCtx(t *testing.T) sdk.Context { return makeTestContext(t) } // IsCheckTx false

// frozenClock pins the token bucket's clock; advance moves it forward.
type frozenClock struct{ now time.Time }

func (c *frozenClock) Now() time.Time          { return c.now }
func (c *frozenClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// withGuard sets the guard's tuning for the test (generous by default).
func withGuard(t *testing.T, cacheSize int, rate, burst float64) *frozenClock {
	clock := &frozenClock{now: time.Unix(1_700_000_000, 0)}
	t.Cleanup(shieldante.SetProofGuardForTest(cacheSize, rate, burst, clock.Now))
	return clock
}

func anteRun(d shieldante.ShieldGasDecorator, ctx sdk.Context, msg *shieldtypes.MsgShieldedExec, simulate bool) error {
	_, err := d.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, simulate, terminalHandler)
	return err
}

// A proof the pairing check rejected is not verified again when resubmitted.
func TestShieldGasDecorator_RejectedProofCached(t *testing.T) {
	withGuard(t, 16, 1000, 1000)
	sk := newGuardKeeper()
	sk.result = shieldtypes.ErrInvalidProof
	d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

	for i := 0; i < 5; i++ {
		require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
	}
	require.Equal(t, 1, sk.calls, "verified once, then served from the cache")
	require.Equal(t, 1, shieldante.RejectedProofCacheLen(d))

	// ReCheckTx and simulate read the cache too.
	require.ErrorIs(t, anteRun(d, recheckCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), true), shieldtypes.ErrInvalidProof)
	require.Equal(t, 1, sk.calls)

	// Any change to the message is a different key.
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(2), false), shieldtypes.ErrInvalidProof)
	require.Equal(t, 2, sk.calls)
}

// DeliverTx never reads or writes the cache: consensus must not depend on
// node-local memory.
func TestShieldGasDecorator_RejectedProofCacheNotUsedInDeliverTx(t *testing.T) {
	withGuard(t, 16, 1000, 1000)
	sk := newGuardKeeper()
	sk.result = shieldtypes.ErrInvalidProof
	d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

	// Rejections in DeliverTx are not cached.
	require.ErrorIs(t, anteRun(d, deliverCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
	require.ErrorIs(t, anteRun(d, deliverCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
	require.Equal(t, 2, sk.calls)
	require.Zero(t, shieldante.RejectedProofCacheLen(d))

	// A CheckTx rejection is cached, but DeliverTx still verifies.
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
	require.Equal(t, 1, shieldante.RejectedProofCacheLen(d))
	require.ErrorIs(t, anteRun(d, deliverCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
	require.Equal(t, 4, sk.calls)

	// And if the proof verifies there, it is accepted.
	sk.result = nil
	require.NoError(t, anteRun(d, deliverCtx(t), guardedExec(1), false))
}

// Only proof-verification failures are cached: anything state-dependent can
// change and must be re-checked.
func TestShieldGasDecorator_StateFailuresNotCached(t *testing.T) {
	for _, stateErr := range []error{
		shieldtypes.ErrInvalidMerkleRoot,
		shieldtypes.ErrNullifierUsed,
		shieldtypes.ErrRateLimitExceeded,
		shieldtypes.ErrShieldDisabled,
		shieldtypes.ErrUnregisteredOperation,
		shieldtypes.ErrOperationInactive,
		shieldtypes.ErrInsufficientTrustLevel,
		shieldtypes.ErrNoVerificationKey,
	} {
		t.Run(stateErr.Error(), func(t *testing.T) {
			withGuard(t, 16, 1000, 1000)
			sk := newGuardKeeper()
			sk.result = stateErr
			d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

			require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), stateErr)
			require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), stateErr)
			require.Equal(t, 2, sk.calls)
			require.Zero(t, shieldante.RejectedProofCacheLen(d))

			// Once the state allows it, the same exec passes.
			sk.result = nil
			require.NoError(t, anteRun(d, checkCtx(t), guardedExec(1), false))
		})
	}
}

// Ownership-mode proofs bind the owner record's scope and sequence, which the
// key can't cover, so their rejections are never cached.
func TestShieldGasDecorator_OwnershipRejectionNotCached(t *testing.T) {
	withGuard(t, 16, 1000, 1000)
	sk := newGuardKeeper()
	sk.reg.NullifierMode = shieldtypes.NullifierMode_NULLIFIER_MODE_OWNERSHIP
	sk.result = shieldtypes.ErrInvalidProof
	d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
	require.Equal(t, 2, sk.calls)
	require.Zero(t, shieldante.RejectedProofCacheLen(d))
}

// The key covers the verification key, the epoch and the op registration:
// changing any of them re-verifies a previously rejected exec.
func TestShieldGasDecorator_RejectedProofKeyInvalidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*guardKeeper)
	}{
		{"verification key replaced", func(g *guardKeeper) { g.vk = []byte("verification-key-v2") }},
		{"epoch advanced", func(g *guardKeeper) { g.epoch++ }},
		{"registration changed", func(g *guardKeeper) { g.reg.NullifierScopeType = shieldtypes.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withGuard(t, 16, 1000, 1000)
			sk := newGuardKeeper()
			sk.result = shieldtypes.ErrInvalidProof
			d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

			require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
			require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), shieldtypes.ErrInvalidProof)
			require.Equal(t, 1, sk.calls)

			tc.change(sk)
			sk.result = nil // valid under the new key/epoch/registration
			require.NoError(t, anteRun(d, checkCtx(t), guardedExec(1), false))
			require.Equal(t, 2, sk.calls)
		})
	}
}

// The cache is bounded and evicts the least recently used entry.
func TestShieldGasDecorator_RejectedProofCacheBounded(t *testing.T) {
	withGuard(t, 2, 1000, 1000)
	sk := newGuardKeeper()
	sk.result = shieldtypes.ErrInvalidProof
	d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

	for seed := byte(1); seed <= 3; seed++ {
		require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(seed), false), shieldtypes.ErrInvalidProof)
	}
	require.Equal(t, 2, shieldante.RejectedProofCacheLen(d))
	require.Equal(t, 3, sk.calls)

	// 3 and 2 are cached; 1 was evicted.
	require.Error(t, anteRun(d, checkCtx(t), guardedExec(3), false))
	require.Error(t, anteRun(d, checkCtx(t), guardedExec(2), false))
	require.Equal(t, 3, sk.calls)
	require.Error(t, anteRun(d, checkCtx(t), guardedExec(1), false))
	require.Equal(t, 4, sk.calls)
}

// New CheckTx (and simulate) verifications are rate limited; ReCheckTx and
// DeliverTx never are.
func TestShieldGasDecorator_VerificationThrottle(t *testing.T) {
	clock := withGuard(t, 16, 1, 3) // 1/s sustained, burst of 3
	sk := newGuardKeeper()          // every proof valid
	d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

	for seed := byte(1); seed <= 3; seed++ {
		require.NoError(t, anteRun(d, checkCtx(t), guardedExec(seed), false))
	}
	err := anteRun(d, checkCtx(t), guardedExec(4), false)
	require.ErrorIs(t, err, shieldtypes.ErrProofVerificationThrottled)
	require.Equal(t, 3, sk.calls, "a throttled exec is not verified")
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(4), true), shieldtypes.ErrProofVerificationThrottled, "simulate is throttled too")

	// ReCheckTx (re-validating mempool txs) and DeliverTx are never throttled.
	require.NoError(t, anteRun(d, recheckCtx(t), guardedExec(1), false))
	require.NoError(t, anteRun(d, deliverCtx(t), guardedExec(1), false))
	require.Equal(t, 5, sk.calls)

	// A throttled rejection isn't cached: the exec passes once tokens refill.
	clock.advance(time.Second)
	require.NoError(t, anteRun(d, checkCtx(t), guardedExec(4), false))
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(5), false), shieldtypes.ErrProofVerificationThrottled)

	// Refill is capped at the burst.
	clock.advance(time.Hour)
	for seed := byte(10); seed < 13; seed++ {
		require.NoError(t, anteRun(d, checkCtx(t), guardedExec(seed), false))
	}
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(13), false), shieldtypes.ErrProofVerificationThrottled)
}

// Execs that fail a cheap state check before any verification don't use up
// tokens; rejected proofs (a verification ran) do.
func TestShieldGasDecorator_ThrottleCountsOnlyVerifications(t *testing.T) {
	withGuard(t, 16, 1, 1)
	sk := newGuardKeeper()
	d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

	sk.result = shieldtypes.ErrNullifierUsed
	for i := 0; i < 10; i++ {
		require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(1), false), shieldtypes.ErrNullifierUsed)
	}

	sk.result = shieldtypes.ErrInvalidProof
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(2), false), shieldtypes.ErrInvalidProof)
	sk.result = nil
	require.ErrorIs(t, anteRun(d, checkCtx(t), guardedExec(3), false), shieldtypes.ErrProofVerificationThrottled)
}

// With no verification key (test-mode builds) nothing is verified, so the
// guard stands aside entirely.
func TestShieldGasDecorator_GuardIdleWithoutVK(t *testing.T) {
	withGuard(t, 16, 0, 0) // a bucket that never admits anything
	sk := newGuardKeeper()
	sk.vk = nil
	d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

	require.NoError(t, anteRun(d, checkCtx(t), guardedExec(1), false))
	sk.result = shieldtypes.ErrInvalidProof
	require.Error(t, anteRun(d, checkCtx(t), guardedExec(1), false))
	require.Error(t, anteRun(d, checkCtx(t), guardedExec(1), false))
	require.Equal(t, 3, sk.calls)
	require.Zero(t, shieldante.RejectedProofCacheLen(d))
}

// The guard is safe under concurrent CheckTx / simulate calls.
func TestShieldGasDecorator_GuardConcurrent(t *testing.T) {
	withGuard(t, 8, 1000, 1000)
	sk := newGuardKeeper()
	sk.result = shieldtypes.ErrInvalidProof
	d := shieldante.NewShieldGasDecorator(sk, &mockBankKeeper{})

	// Prime the cache, then hit it from many goroutines (run with -race).
	// Cache hits never reach the keeper, whose counter isn't synchronised.
	require.Error(t, anteRun(d, checkCtx(t), guardedExec(0), false))
	ctx := checkCtx(t)
	done := make(chan struct{})
	for g := 0; g < 8; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 50; i++ {
				_ = anteRun(d, ctx, guardedExec(0), i%2 == 0)
			}
		}()
	}
	for g := 0; g < 8; g++ {
		<-done
	}
	require.Equal(t, 1, sk.calls)
}
