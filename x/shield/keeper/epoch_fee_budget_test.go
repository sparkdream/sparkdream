package keeper_test

import (
	"encoding/hex"
	"testing"

	"cosmossdk.io/collections"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// feeExec is an exec whose fee is charged to identity id (its rate-limit
// nullifier) in the given proof domain.
func feeExec(id byte, domain types.ProofDomain) *types.MsgShieldedExec {
	rl := make([]byte, 32)
	rl[31] = id
	return &types.MsgShieldedExec{RateLimitNullifier: rl, ProofDomain: domain}
}

// TestChargeEpochFee (SHD-12): fees the module pays are booked per shield
// epoch against max_fees_per_epoch; a fee that doesn't fit is refused and not
// booked, and the budget is fresh in the next epoch.
func TestChargeEpochFee(t *testing.T) {
	f := initFixture(t)
	params := types.DefaultParams()
	params.MaxFeesPerEpoch = math.NewInt(100)
	params.MaxFeePerExec = math.NewInt(60)
	group := types.ProofDomain_PROOF_DOMAIN_GROUP_TREE
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 7, EpochStartHeight: 1}))

	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(60), params, feeExec(1, group)))
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(40), params, feeExec(2, group)))
	require.Equal(t, math.NewInt(100), f.keeper.EpochFeesPaid(f.ctx))
	require.ErrorIs(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(1), params, feeExec(3, group)), types.ErrEpochFeeBudgetExhausted)
	require.Equal(t, math.NewInt(100), f.keeper.EpochFeesPaid(f.ctx), "a refused fee is not booked")
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.ZeroInt(), params, feeExec(3, group)), "zero fees cost nothing")

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 8, EpochStartHeight: 51}))
	require.True(t, f.keeper.EpochFeesPaid(f.ctx).IsZero())
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(60), params, feeExec(1, group)))
}

// TestEpochFeeBudgetShares (SHD-102): one identity may use only its share of
// max_fees_per_epoch, so the 13 max-fee execs (failing or not) that used to
// take the whole mainnet budget no longer lock a second member out; and
// trust-tree traffic stops short of the group-tree reserve, so secret-ballot
// votes still get paid for when every other exec is refused.
func TestEpochFeeBudgetShares(t *testing.T) {
	f := initFixture(t)
	params := types.DefaultParams()
	params.MaxFeePerExec = math.NewInt(50_000) // mainnet genesis
	params.MaxFeesPerEpoch = math.NewInt(650_000)
	trust := types.ProofDomain_PROOF_DOMAIN_TRUST_TREE
	group := types.ProofDomain_PROOF_DOMAIN_GROUP_TREE
	maxFee := params.MaxFeePerExec
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 3, EpochStartHeight: 1}))

	share := types.IdentityEpochFeeCap(params)
	require.Equal(t, math.NewInt(65_000), share)

	// The attacker: one max-fee exec fits its share, the second does not.
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, maxFee, params, feeExec(1, trust)))
	for i := 0; i < 12; i++ {
		require.ErrorIs(t, f.keeper.ChargeEpochFee(f.ctx, maxFee, params, feeExec(1, trust)), types.ErrIdentityFeeShareExhausted)
	}
	paid, err := f.keeper.IdentityEpochFeesPaid(f.ctx, hex.EncodeToString(feeExec(1, trust).RateLimitNullifier))
	require.NoError(t, err)
	require.Equal(t, maxFee, paid)

	// A second member is still paid for in the same epoch.
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, maxFee, params, feeExec(2, trust)))

	// Many members fill the trust-tree part of the budget (budget less the
	// group-tree reserve)...
	limit := types.EpochFeeLimit(params, false)
	require.Equal(t, math.NewInt(520_000), limit)
	id := byte(3)
	for f.keeper.EpochFeesPaid(f.ctx).Add(maxFee).LTE(limit) {
		require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, maxFee, params, feeExec(id, trust)))
		id++
	}
	require.ErrorIs(t, f.keeper.ChargeEpochFee(f.ctx, maxFee, params, feeExec(id, trust)), types.ErrEpochFeeBudgetExhausted)

	// ...but a secret-ballot vote still gets through on the reserve.
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, maxFee, params, feeExec(200, group)))

	// The ledger is per epoch and pruned with the rate limits.
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 4, EpochStartHeight: 51}))
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, maxFee, params, feeExec(1, trust)))
	require.NoError(t, f.keeper.PruneIdentityEpochFees(f.ctx, 4))
	n := 0
	require.NoError(t, f.keeper.IdentityEpochFees.Walk(f.ctx, nil, func(key collections.Pair[uint64, string], _ math.Int) (bool, error) {
		require.Equal(t, uint64(4), key.K1())
		n++
		return false, nil
	}))
	require.Equal(t, 1, n)
}

// TestMaxFeesPerEpochValidation: the epoch budget must exist and cover at
// least one max-fee exec.
func TestMaxFeesPerEpochValidation(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	p.MaxFeesPerEpoch = p.MaxFeePerExec.SubRaw(1)
	require.Error(t, p.Validate())
	p.MaxFeesPerEpoch = math.Int{}
	require.Error(t, p.Validate())
}

// TestRefundEpochFee (SHD-102): the post handler unbooks the part of a fee
// that gas used did not cost from the epoch ledger and the identity's share,
// so both measure gas used. Both saturate at zero, and nothing is unbooked
// across an epoch boundary.
func TestRefundEpochFee(t *testing.T) {
	f := initFixture(t)
	params := types.DefaultParams()
	params.MaxFeesPerEpoch = math.NewInt(1000)
	params.MaxFeePerExec = math.NewInt(100)
	trust := types.ProofDomain_PROOF_DOMAIN_TRUST_TREE
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 7, EpochStartHeight: 1}))
	id1 := hex.EncodeToString(feeExec(1, trust).RateLimitNullifier)
	id2 := hex.EncodeToString(feeExec(2, trust).RateLimitNullifier)
	idPaid := func(id string) math.Int {
		paid, err := f.keeper.IdentityEpochFeesPaid(f.ctx, id)
		require.NoError(t, err)
		return paid
	}

	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(100), params, feeExec(1, trust)))
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(100), params, feeExec(2, trust)))

	// Decrement: both books drop by the refund, the other identity is untouched.
	require.NoError(t, f.keeper.RefundEpochFee(f.ctx, 7, math.NewInt(60), id1))
	require.Equal(t, math.NewInt(140), f.keeper.EpochFeesPaid(f.ctx))
	require.Equal(t, math.NewInt(40), idPaid(id1))
	require.Equal(t, math.NewInt(100), idPaid(id2))

	// The refund gives the share back: a 100 fee fits id1 again (share = 100).
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(60), params, feeExec(1, trust)))
	require.Equal(t, math.NewInt(100), idPaid(id1))

	// Zero or nil refunds are no-ops.
	require.NoError(t, f.keeper.RefundEpochFee(f.ctx, 7, math.ZeroInt(), id1))
	require.NoError(t, f.keeper.RefundEpochFee(f.ctx, 7, math.Int{}, id1))
	require.Equal(t, math.NewInt(200), f.keeper.EpochFeesPaid(f.ctx))

	// Saturation: more than was booked leaves zero, never a negative ledger.
	require.NoError(t, f.keeper.RefundEpochFee(f.ctx, 7, math.NewInt(150), id2))
	require.True(t, idPaid(id2).IsZero())
	require.Equal(t, math.NewInt(50), f.keeper.EpochFeesPaid(f.ctx))
	require.NoError(t, f.keeper.RefundEpochFee(f.ctx, 7, math.NewInt(1_000_000), id1))
	require.True(t, idPaid(id1).IsZero())
	require.True(t, f.keeper.EpochFeesPaid(f.ctx).IsZero())

	// An identity never charged is left alone (no entry is created).
	require.NoError(t, f.keeper.RefundEpochFee(f.ctx, 7, math.NewInt(10), hex.EncodeToString(feeExec(9, trust).RateLimitNullifier)))
	_, err := f.keeper.IdentityEpochFees.Get(f.ctx, collections.Join(uint64(7), hex.EncodeToString(feeExec(9, trust).RateLimitNullifier)))
	require.ErrorIs(t, err, collections.ErrNotFound)

	// Epoch mismatch: a refund for epoch 7 after the epoch moved to 8 unbooks
	// nothing, in either epoch's books.
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(100), params, feeExec(1, trust)))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 8, EpochStartHeight: 51}))
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(100), params, feeExec(1, trust)))
	require.NoError(t, f.keeper.RefundEpochFee(f.ctx, 7, math.NewInt(100), id1))
	require.Equal(t, math.NewInt(100), f.keeper.EpochFeesPaid(f.ctx), "epoch 8's ledger untouched")
	require.Equal(t, math.NewInt(100), idPaid(id1), "epoch 8's share untouched")
	old, err := f.keeper.IdentityEpochFees.Get(f.ctx, collections.Join(uint64(7), id1))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100), old, "epoch 7's share untouched")
	// and a refund for the current epoch against a ledger still in the old
	// epoch (no fee charged yet in the new one) unbooks only the identity
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 9, EpochStartHeight: 101}))
	require.NoError(t, f.keeper.IdentityEpochFees.Set(f.ctx, collections.Join(uint64(9), id1), math.NewInt(30)))
	require.NoError(t, f.keeper.RefundEpochFee(f.ctx, 9, math.NewInt(10), id1))
	require.Equal(t, math.NewInt(20), idPaid(id1))
	ledger, err := f.keeper.EpochFeeLedger.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(8), ledger.Epoch)
	require.Equal(t, math.NewInt(100), ledger.Paid, "a stale ledger is not rewritten")
}
