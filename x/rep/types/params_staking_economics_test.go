package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestStakingEconomicsParams_Defaults pins the shipped values of the seven
// params added with the seasonal-pool and completion-bonus work, and the two
// decay/yield rates they are calibrated against.
//
// The figures are coupled, not independent, so pinning them here is what makes
// a later edit to one of them a deliberate act rather than a silent drift:
//
//   - min_stake_amount is 1/staking_reward_yield_per_epoch, the smallest stake
//     whose own per-epoch accrual reaches one micro-DREAM;
//   - staked_decay_rate is half the yield, so a staked position nets a positive
//     but modest +0.025%/epoch;
//   - staking_pool_cap_base is the genesis DREAM supply.
func TestStakingEconomicsParams_Defaults(t *testing.T) {
	p := types.DefaultParams()

	require.Equal(t, math.LegacyNewDecWithPrec(5, 4), p.StakingRewardYieldPerEpoch, "0.05%/epoch")
	require.Equal(t, math.LegacyNewDecWithPrec(25, 5), p.StakedDecayRate, "0.025%/epoch")
	require.Equal(t, math.LegacyNewDecWithPrec(2, 3), p.UnstakedDecayRate, "0.2%/epoch")
	require.Equal(t, math.LegacyNewDecWithPrec(5, 2), p.StakingPoolMintShare)
	require.Equal(t, math.NewInt(25_000_000_000), p.StakingPoolCapBase)
	require.Equal(t, math.LegacyNewDecWithPrec(5, 2), p.StakingPoolCapRate)
	require.Equal(t, math.NewInt(2_000), p.MinStakeAmount)
	require.Equal(t, math.LegacyNewDec(3), p.MaxCompletionBonusStakeMultiple)
	require.Equal(t, math.NewInt(20_000_000_000), p.MaxTotalContentStakePerMember)

	// The floor is derived from the yield, not chosen independently: below
	// 1/yield a stake's own accrual truncates to zero every epoch.
	require.Equal(t, math.LegacyOneDec().Quo(p.StakingRewardYieldPerEpoch).TruncateInt(), p.MinStakeAmount,
		"min_stake_amount must stay pinned to 1/staking_reward_yield_per_epoch")

	// A staked position must net positive, or staking is strictly worse than
	// holding the same DREAM locked in escrow.
	require.True(t, p.StakingRewardYieldPerEpoch.GT(p.StakedDecayRate),
		"the yield must exceed staked decay or staking loses money")

	// And staking must beat NOT staking, which is what unstaked decay enforces.
	gradient := p.StakingRewardYieldPerEpoch.Sub(p.StakedDecayRate).Add(p.UnstakedDecayRate)
	require.True(t, gradient.IsPositive(), "staking must dominate holding unstaked")

	require.NoError(t, p.Validate())
	require.NoError(t, types.DefaultRepOperationalParams().Validate())
}

// TestStakingEconomicsParams_ValidateBounds covers Validate() for each of the
// seven, on both Params and RepOperationalParams — the two must agree, since
// ApplyOperationalParams writes the operational value straight onto Params.
func TestStakingEconomicsParams_ValidateBounds(t *testing.T) {
	base := types.DefaultParams()
	opBase := types.DefaultRepOperationalParams()

	t.Run("dec rates in [0,1]", func(t *testing.T) {
		for _, field := range []struct {
			name string
			set  func(p *types.Params, v math.LegacyDec)
			setO func(op *types.RepOperationalParams, v math.LegacyDec)
		}{
			{"staking_reward_yield_per_epoch",
				func(p *types.Params, v math.LegacyDec) { p.StakingRewardYieldPerEpoch = v },
				func(op *types.RepOperationalParams, v math.LegacyDec) { op.StakingRewardYieldPerEpoch = v }},
			{"staking_pool_mint_share",
				func(p *types.Params, v math.LegacyDec) { p.StakingPoolMintShare = v },
				func(op *types.RepOperationalParams, v math.LegacyDec) { op.StakingPoolMintShare = v }},
		} {
			t.Run(field.name, func(t *testing.T) {
				for _, tc := range []struct {
					name    string
					v       math.LegacyDec
					wantErr bool
				}{
					{"zero is allowed", math.LegacyZeroDec(), false},
					{"one is allowed", math.LegacyOneDec(), false},
					{"negative is rejected", math.LegacyNewDecWithPrec(-1, 3), true},
					{"above one is rejected", math.LegacyNewDec(2), true},
					{"nil is rejected", math.LegacyDec{}, true},
				} {
					t.Run(tc.name, func(t *testing.T) {
						p := base
						field.set(&p, tc.v)
						op := opBase
						field.setO(&op, tc.v)
						if tc.wantErr {
							require.Error(t, p.Validate())
							require.Error(t, op.Validate())
						} else {
							require.NoError(t, p.Validate())
							require.NoError(t, op.Validate())
						}
					})
				}
			})
		}
	})

	t.Run("staking_pool_cap_rate rejects only negatives", func(t *testing.T) {
		// Unlike the two above this is NOT capped at 1: the schedule ceiling is
		// cap_base * (season+1) * cap_rate, and a rate above 1 is a coherent
		// (if aggressive) schedule rather than a malformed one.
		for _, tc := range []struct {
			name    string
			v       math.LegacyDec
			wantErr bool
		}{
			{"zero is allowed", math.LegacyZeroDec(), false},
			{"above one is allowed", math.LegacyNewDec(2), false},
			{"negative is rejected", math.LegacyNewDec(-1), true},
			{"nil is rejected", math.LegacyDec{}, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.StakingPoolCapRate = tc.v
				op := opBase
				op.StakingPoolCapRate = tc.v
				if tc.wantErr {
					require.Error(t, p.Validate())
					require.Error(t, op.Validate())
				} else {
					require.NoError(t, p.Validate())
					require.NoError(t, op.Validate())
				}
			})
		}
	})

	t.Run("staking_pool_cap_base rejects only negatives", func(t *testing.T) {
		// Zero is allowed and meaningful: it collapses the schedule ceiling to
		// zero, ending staking emission without removing the param.
		for _, tc := range []struct {
			name    string
			v       math.Int
			wantErr bool
		}{
			{"zero is allowed", math.ZeroInt(), false},
			{"positive is allowed", math.NewInt(1), false},
			{"negative is rejected", math.NewInt(-1), true},
			{"nil is rejected", math.Int{}, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.StakingPoolCapBase = tc.v
				op := opBase
				op.StakingPoolCapBase = tc.v
				if tc.wantErr {
					require.Error(t, p.Validate())
					require.Error(t, op.Validate())
				} else {
					require.NoError(t, p.Validate())
					require.NoError(t, op.Validate())
				}
			})
		}
	})

	t.Run("min_stake_amount", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			v       math.Int
			wantErr bool
		}{
			{"positive is allowed", math.NewInt(1), false},
			{"equal to the per-member cap is allowed", base.MaxInitiativeStakePerMember, false},
			{"zero is rejected — an unset floor is not permission to accept dust", math.ZeroInt(), true},
			{"negative is rejected", math.NewInt(-1), true},
			{"nil is rejected", math.Int{}, true},
			{"above the per-member stake cap is rejected", base.MaxInitiativeStakePerMember.AddRaw(1), true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.MinStakeAmount = tc.v
				op := opBase
				op.MinStakeAmount = tc.v
				if tc.wantErr {
					require.Error(t, p.Validate())
					require.Error(t, op.Validate())
				} else {
					require.NoError(t, p.Validate())
					require.NoError(t, op.Validate())
				}
			})
		}
	})

	t.Run("max_completion_bonus_stake_multiple", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			v       math.LegacyDec
			wantErr bool
		}{
			{"zero is allowed and disables the bonus", math.LegacyZeroDec(), false},
			{"above one is allowed — it is a multiple, not a ratio", math.LegacyNewDec(10), false},
			{"negative is rejected", math.LegacyNewDec(-1), true},
			{"nil is rejected", math.LegacyDec{}, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.MaxCompletionBonusStakeMultiple = tc.v
				op := opBase
				op.MaxCompletionBonusStakeMultiple = tc.v
				if tc.wantErr {
					require.Error(t, p.Validate())
					require.Error(t, op.Validate())
				} else {
					require.NoError(t, p.Validate())
					require.NoError(t, op.Validate())
				}
			})
		}
	})

	t.Run("max_total_content_stake_per_member", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			v       math.Int
			wantErr bool
		}{
			{"equal to the per-item cap is allowed", base.MaxContentStakePerMember, false},
			{"above the per-item cap is allowed", base.MaxContentStakePerMember.MulRaw(2), false},
			{"below the per-item cap is rejected — a single stake could not fit", base.MaxContentStakePerMember.SubRaw(1), true},
			{"zero is rejected", math.ZeroInt(), true},
			{"negative is rejected", math.NewInt(-1), true},
			{"nil is rejected", math.Int{}, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.MaxTotalContentStakePerMember = tc.v
				op := opBase
				op.MaxTotalContentStakePerMember = tc.v
				if tc.wantErr {
					require.Error(t, p.Validate())
					require.Error(t, op.Validate())
				} else {
					require.NoError(t, p.Validate())
					require.NoError(t, op.Validate())
				}
			})
		}
	})
}

// TestStakingEconomicsParams_OperationalRoundTrip covers the other half of the
// contract: every one of the seven must survive ExtractOperationalParams ->
// ApplyOperationalParams unchanged. A param added to Params but missed in
// either direction silently reverts to its old value on the next
// MsgUpdateOperationalParams, which is exactly the class of bug the
// full-replacement semantics make invisible.
func TestStakingEconomicsParams_OperationalRoundTrip(t *testing.T) {
	p := types.DefaultParams()

	p.StakingRewardYieldPerEpoch = math.LegacyNewDecWithPrec(7, 4)
	p.StakingPoolMintShare = math.LegacyNewDecWithPrec(11, 2)
	p.StakingPoolCapBase = math.NewInt(42_000_000_000)
	p.StakingPoolCapRate = math.LegacyNewDecWithPrec(13, 2)
	p.MinStakeAmount = math.NewInt(3_333)
	p.MaxCompletionBonusStakeMultiple = math.LegacyNewDecWithPrec(45, 1)
	p.MaxTotalContentStakePerMember = math.NewInt(31_000_000_000)
	require.NoError(t, p.Validate())

	op := p.ExtractOperationalParams()
	require.NoError(t, op.Validate())

	restored := types.DefaultParams().ApplyOperationalParams(op)

	require.Equal(t, p.StakingRewardYieldPerEpoch, restored.StakingRewardYieldPerEpoch)
	require.Equal(t, p.StakingPoolMintShare, restored.StakingPoolMintShare)
	require.Equal(t, p.StakingPoolCapBase, restored.StakingPoolCapBase)
	require.Equal(t, p.StakingPoolCapRate, restored.StakingPoolCapRate)
	require.Equal(t, p.MinStakeAmount, restored.MinStakeAmount)
	require.Equal(t, p.MaxCompletionBonusStakeMultiple, restored.MaxCompletionBonusStakeMultiple)
	require.Equal(t, p.MaxTotalContentStakePerMember, restored.MaxTotalContentStakePerMember)
}
