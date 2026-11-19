package app

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// TreasuryAddressSource lists the addresses whose delegations are group
// treasury stake (x/commons Keeper.TreasuryAddresses).
type TreasuryAddressSource interface {
	TreasuryAddresses(ctx context.Context) ([]string, error)
}

// TallyStakingKeeper is the slice of x/staking the gov tally reads.
type TallyStakingKeeper interface {
	IterateDelegations(ctx context.Context, delegator sdk.AccAddress, fn func(index int64, delegation stakingtypes.DelegationI) (stop bool)) error
	ValidatorAddressCodec() address.Codec
}

// NewTreasuryExcludingTally returns the x/gov vote tally of this chain. It
// counts votes like the SDK's default tally, with one difference: SPARK that
// a council or committee treasury has delegated (x/commons treasury staking)
// has no voting power. It is taken out of the voting power its validator
// inherits, and a treasury address that votes itself counts for nothing.
//
// Without this the group treasuries, which hold most of the supply and are
// children of x/gov, could raise a validator's weight in x/gov votes by
// delegating to it.
//
// What the function cannot change is the quorum denominator: x/gov divides
// the voting power returned here by total bonded stake, which still includes
// treasury stake. Treasury stake therefore counts as stake that did not
// vote; the treasury stake cap (commons TreasuryStakeCapBps) bounds how much
// harder that can make a quorum.
//
// The getters are called at tally time, so the keepers may be wired after
// the gov keeper is built.
func NewTreasuryExcludingTally(
	treasuries func() TreasuryAddressSource,
	staking func() TallyStakingKeeper,
	addressCodec address.Codec,
) govkeeper.CalculateVoteResultsAndVotingPowerFn {
	return func(
		ctx context.Context,
		k govkeeper.Keeper,
		proposal govv1.Proposal,
		validators map[string]govv1.ValidatorGovInfo,
	) (math.LegacyDec, map[govv1.VoteOption]math.LegacyDec, error) {
		sk := staking()
		totalVotingPower := math.LegacyZeroDec()
		results := map[govv1.VoteOption]math.LegacyDec{
			govv1.OptionYes:        math.LegacyZeroDec(),
			govv1.OptionAbstain:    math.LegacyZeroDec(),
			govv1.OptionNo:         math.LegacyZeroDec(),
			govv1.OptionNoWithVeto: math.LegacyZeroDec(),
		}
		addVote := func(power math.LegacyDec, options govv1.WeightedVoteOptions) {
			for _, option := range options {
				weight, err := math.LegacyNewDecFromStr(option.Weight)
				if err != nil {
					continue
				}
				results[option.Option] = results[option.Option].Add(power.Mul(weight))
			}
			totalVotingPower = totalVotingPower.Add(power)
		}

		// 1. Take treasury delegations out of every validator's inherited
		// power, whether or not anyone votes for them.
		treasuryAddrs, err := treasuries().TreasuryAddresses(ctx)
		if err != nil {
			return math.LegacyZeroDec(), nil, fmt.Errorf("reading treasury addresses: %w", err)
		}
		isTreasury := make(map[string]bool, len(treasuryAddrs))
		for _, addr := range treasuryAddrs {
			bz, err := addressCodec.StringToBytes(addr)
			if err != nil || isTreasury[addr] {
				continue
			}
			isTreasury[addr] = true
			if err := sk.IterateDelegations(ctx, bz, func(_ int64, delegation stakingtypes.DelegationI) bool {
				if val, ok := validators[delegation.GetValidatorAddr()]; ok {
					val.DelegatorDeductions = val.DelegatorDeductions.Add(delegation.GetShares())
					validators[delegation.GetValidatorAddr()] = val
				}
				return false
			}); err != nil {
				return math.LegacyZeroDec(), nil, fmt.Errorf("iterating treasury delegations: %w", err)
			}
		}

		// 2. Count each voter's own delegations and take them out of the
		// validators they are delegated to.
		rng := collections.NewPrefixedPairRange[uint64, sdk.AccAddress](proposal.Id)
		var votesToRemove []collections.Pair[uint64, sdk.AccAddress]
		err = k.Votes.Walk(ctx, rng, func(key collections.Pair[uint64, sdk.AccAddress], vote govv1.Vote) (bool, error) {
			votesToRemove = append(votesToRemove, key)

			voter, err := addressCodec.StringToBytes(vote.Voter)
			if err != nil {
				return false, err
			}
			valAddr, err := sk.ValidatorAddressCodec().BytesToString(voter)
			if err != nil {
				return false, err
			}
			if val, ok := validators[valAddr]; ok {
				val.Vote = vote.Options
				validators[valAddr] = val
			}

			// A treasury's stake was removed in step 1 and has no power.
			if isTreasury[vote.Voter] {
				return false, nil
			}

			return false, sk.IterateDelegations(ctx, voter, func(_ int64, delegation stakingtypes.DelegationI) bool {
				val, ok := validators[delegation.GetValidatorAddr()]
				if !ok || val.DelegatorShares.IsZero() {
					return false
				}
				val.DelegatorDeductions = val.DelegatorDeductions.Add(delegation.GetShares())
				validators[delegation.GetValidatorAddr()] = val
				addVote(delegation.GetShares().MulInt(val.BondedTokens).Quo(val.DelegatorShares), vote.Options)
				return false
			})
		})
		if err != nil {
			return math.LegacyZeroDec(), nil, fmt.Errorf("iterating votes: %w", err)
		}
		for _, key := range votesToRemove {
			if err := k.Votes.Remove(ctx, key); err != nil {
				return math.LegacyZeroDec(), nil, fmt.Errorf("removing vote (%d/%s): %w", key.K1(), key.K2(), err)
			}
		}

		// 3. Validators that voted carry the stake of their delegators who
		// did not vote, treasuries excepted.
		for _, val := range validators {
			if len(val.Vote) == 0 || val.DelegatorShares.IsZero() {
				continue
			}
			shares := val.DelegatorShares.Sub(val.DelegatorDeductions)
			if !shares.IsPositive() {
				continue
			}
			addVote(shares.MulInt(val.BondedTokens).Quo(val.DelegatorShares), val.Vote)
		}

		return totalVotingPower, results, nil
	}
}
