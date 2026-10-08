package ante

import (
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"sync"
	"time"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	shieldtypes "sparkdream/x/shield/types"
)

// Mempool CPU guard for immediate-mode proof verification (review finding M4).
//
// The ante handler verifies an immediate exec's Groth16 proof in CheckTx so
// the shield module never pays for an invalid one. Txs signed by the public
// submitter are free to submit (an invalid proof is rejected before any fee is
// paid), so without a guard anyone can make every node run a pairing check per
// submission, forever. Two node-local, non-consensus defences sit in front of
// PrecheckImmediate, both used only in CheckTx / ReCheckTx / simulate and
// never in DeliverTx (FinalizeBlock), so consensus never depends on them:
//
//  1. A bounded LRU of execs whose proof the pairing check rejected. An
//     identical resubmission is refused without a second verification.
//  2. A token bucket capping how many verifications new CheckTx (and
//     simulate) runs per second. ReCheckTx is exempt: throttling it would
//     evict txs that already passed.

var (
	// rejectedProofCacheSize bounds the rejected-proof cache. Entries are a
	// 32-byte key plus list/map overhead (~150 bytes), so 4096 entries cost
	// well under 1 MiB.
	rejectedProofCacheSize = 4096

	// proofVerifyRate is the sustained number of Groth16 verifications new
	// CheckTx may run per second. A BN254 verification of the shield circuit
	// (3 pairings + a 7-input MSM) takes ~1.5-3 ms on one core, so 150/s
	// bounds an attacker to roughly a third to half of one core, while honest
	// anonymous traffic (bounded per identity by
	// max_execs_per_identity_per_epoch) sits far below it.
	proofVerifyRate = 150.0

	// proofVerifyBurst is the token bucket's capacity: up to this many
	// verifications can run back to back (e.g. a flush of queued anonymous
	// txs after a network hiccup) before the sustained rate applies.
	proofVerifyBurst = 300.0

	// nowFunc is the clock the token bucket refills against.
	nowFunc = time.Now
)

// shieldCircuitID is the verification key the keeper verifies immediate execs
// against (see keeper.verifyProof).
const shieldCircuitID = shieldtypes.ShieldCircuitID

// proofGuard is the node-local state shared by every copy of the decorator.
type proofGuard struct {
	rejected *rejectedProofCache
	bucket   *tokenBucket
}

func newProofGuard() *proofGuard {
	return &proofGuard{
		rejected: newRejectedProofCache(rejectedProofCacheSize),
		bucket:   newTokenBucket(proofVerifyRate, proofVerifyBurst),
	}
}

// precheckImmediate runs the keeper's PrecheckImmediate behind the guard.
func (d ShieldGasDecorator) precheckImmediate(ctx sdk.Context, msg *shieldtypes.MsgShieldedExec, simulate bool) error {
	// DeliverTx (FinalizeBlock): consensus must not depend on node-local
	// memory, so always verify.
	consensus := !simulate && !ctx.IsCheckTx() && ctx.ExecMode() != sdk.ExecModeSimulate
	if consensus || d.guard == nil {
		return d.shieldKeeper.PrecheckImmediate(ctx, msg)
	}

	// Without a verification key (test-mode builds) nothing is verified, so
	// there is nothing to guard.
	vk, found := d.shieldKeeper.GetVerificationKeyVal(ctx, shieldCircuitID)
	if !found || len(vk.VkBytes) == 0 {
		return d.shieldKeeper.PrecheckImmediate(ctx, msg)
	}

	key, cacheable := d.rejectedProofKey(ctx, msg, vk.VkBytes)
	if cacheable && d.guard.rejected.contains(key) {
		return errorsmod.Wrap(shieldtypes.ErrInvalidProof, "proof already rejected by this node")
	}

	throttled := !ctx.IsReCheckTx()
	if throttled && !d.guard.bucket.take() {
		return shieldtypes.ErrProofVerificationThrottled
	}

	err := d.shieldKeeper.PrecheckImmediate(ctx, msg)
	switch {
	case err == nil:
	case errors.Is(err, shieldtypes.ErrInvalidProof):
		// In the PrecheckImmediate path ErrInvalidProof comes only from
		// verifying the proof (decoding the VK or proof, building the
		// witness, the pairing check), which is a pure function of what the
		// key covers. Every state-dependent failure (disabled shield, unknown
		// or inactive op, stale root, used nullifier, rate limit) has its own
		// code and is never cached.
		if cacheable {
			d.guard.rejected.add(key)
		}
	default:
		// Failed a cheap state check before any verification ran (they all
		// precede the pairing check), so give the token back.
		if throttled {
			d.guard.bucket.refund()
		}
	}
	return err
}

// rejectedProofKey identifies everything a proof's verification depends on:
//   - the full MsgShieldedExec bytes (the proof and every public input the
//     message carries: merkle root, nullifiers, min trust level, proof domain
//     and the inner message the MessageHash input binds);
//   - the verification key bytes, so a governance VK change invalidates every
//     entry;
//   - the current shield epoch (the RateLimitEpoch public input, and the scope
//     of epoch-scoped ops);
//   - the op registration, which decides the nullifier domain and how the
//     Scope public input is derived from the message.
//
// Ownership-mode ops are not cacheable: their Scope and MessageHash inputs
// come from the target content's owner record (scope, sequence), which is
// state the key cannot cover. Neither is an exec whose op isn't registered
// (PrecheckImmediate rejects it before verifying anyway).
func (d ShieldGasDecorator) rejectedProofKey(ctx sdk.Context, msg *shieldtypes.MsgShieldedExec, vkBytes []byte) ([32]byte, bool) {
	if msg.InnerMessage == nil {
		return [32]byte{}, false
	}
	reg, found := d.shieldKeeper.GetShieldedOp(ctx, msg.InnerMessage.TypeUrl)
	if !found || reg.NullifierMode == shieldtypes.NullifierMode_NULLIFIER_MODE_OWNERSHIP {
		return [32]byte{}, false
	}
	msgBz, err := msg.Marshal()
	if err != nil {
		return [32]byte{}, false
	}
	regBz, err := reg.Marshal()
	if err != nil {
		return [32]byte{}, false
	}

	h := sha256.New()
	h.Write([]byte("sparkdream/shield/rejected-proof/v1"))
	writeLenPrefixed(h, msgBz)
	writeLenPrefixed(h, vkBytes)
	writeLenPrefixed(h, regBz)
	var epoch [8]byte
	binary.BigEndian.PutUint64(epoch[:], d.shieldKeeper.GetCurrentEpoch(ctx))
	h.Write(epoch[:])

	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key, true
}

func writeLenPrefixed(h hash.Hash, bz []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(bz)))
	h.Write(n[:])
	h.Write(bz)
}

// rejectedProofCache is a fixed-size, concurrency-safe LRU set of keys.
type rejectedProofCache struct {
	mu    sync.Mutex
	max   int
	order *list.List // front = most recently used
	items map[[32]byte]*list.Element
}

func newRejectedProofCache(max int) *rejectedProofCache {
	return &rejectedProofCache{max: max, order: list.New(), items: make(map[[32]byte]*list.Element)}
}

func (c *rejectedProofCache) contains(key [32]byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if ok {
		c.order.MoveToFront(e)
	}
	return ok
}

func (c *rejectedProofCache) add(key [32]byte) {
	if c.max <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		c.order.MoveToFront(e)
		return
	}
	c.items[key] = c.order.PushFront(key)
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.([32]byte))
	}
}

func (c *rejectedProofCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// tokenBucket is a concurrency-safe token bucket on nowFunc's clock.
type tokenBucket struct {
	mu     sync.Mutex
	rate   float64 // tokens per second
	burst  float64 // capacity
	tokens float64
	last   time.Time
}

func newTokenBucket(rate, burst float64) *tokenBucket {
	return &tokenBucket{rate: rate, burst: burst, tokens: burst, last: nowFunc()}
}

// take consumes one token, reporting false when none is available.
func (b *tokenBucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := nowFunc()
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = min(b.burst, b.tokens+elapsed*b.rate)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// refund returns a token taken for a verification that never ran.
func (b *tokenBucket) refund() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = min(b.burst, b.tokens+1)
}
