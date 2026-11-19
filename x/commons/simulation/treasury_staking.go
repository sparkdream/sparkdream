package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// Treasury staking (keeper/treasury_staking.go). Delegations, undelegations
// and redelegations are signed by a group policy, so each travels in a
// proposal of the group like every other policy-gated message (council.go);
// the reward withdrawal is signed by any live sim account. Every keeper
// precondition is prechecked here where a wrong pick would be common (a
// member's validator, the stake cap, the staked balance) and by the dry-run
// for the rest (x/staking's own limits on unbonding and redelegation
// entries).

// maxSimTreasuryStake bounds one simulated delegation (1,000 SPARK), so the
// treasuries approach the stake cap over many operations.
var maxSimTreasuryStake = math.NewInt(1_000_000_000)

// treasuryDelegation is one delegation of a group treasury, in tokens.
type treasuryDelegation struct {
	policy    string
	validator string
	tokens    math.Int
}

// treasuryDelegations lists the delegations of every group treasury that
// hold at least one base unit.
func (e *simEnv) treasuryDelegations() ([]treasuryDelegation, error) {
	sk := e.k.TreasuryStakingKeeper()
	if sk == nil {
		return nil, nil
	}
	groups, err := e.groups(func(string, types.Group) bool { return true })
	if err != nil {
		return nil, err
	}
	var out []treasuryDelegation
	for _, g := range groups {
		addr, err := e.ak.AddressCodec().StringToBytes(g.g.PolicyAddress)
		if err != nil {
			continue
		}
		delegations, err := sk.GetDelegatorDelegations(e.ctx, addr, ^uint16(0))
		if err != nil {
			return nil, err
		}
		for _, d := range delegations {
			valBz, err := sk.ValidatorAddressCodec().StringToBytes(d.ValidatorAddress)
			if err != nil {
				continue
			}
			val, err := sk.GetValidator(e.ctx, valBz)
			if err != nil || val.DelegatorShares.IsZero() {
				continue
			}
			if tokens := val.TokensFromShares(d.Shares).TruncateInt(); tokens.IsPositive() {
				out = append(out, treasuryDelegation{policy: g.g.PolicyAddress, validator: d.ValidatorAddress, tokens: tokens})
			}
		}
	}
	return out, nil
}

// outsideValidators returns the bonded validators no member of the group
// operates (the keeper refuses a member's validator), except skip.
func (e *simEnv) outsideValidators(groupName, skip string) ([]stakingtypes.Validator, error) {
	sk := e.k.TreasuryStakingKeeper()
	if sk == nil {
		return nil, nil
	}
	vals, err := sk.GetBondedValidatorsByPower(e.ctx)
	if err != nil {
		return nil, err
	}
	var out []stakingtypes.Validator
	for _, v := range vals {
		if v.OperatorAddress == skip {
			continue
		}
		valBz, err := sk.ValidatorAddressCodec().StringToBytes(v.OperatorAddress)
		if err != nil {
			continue
		}
		operator, err := e.ak.AddressCodec().BytesToString(valBz)
		if err != nil {
			continue
		}
		isMember, err := e.k.HasMember(e.ctx, groupName, operator)
		if err != nil {
			return nil, err
		}
		if !isMember {
			out = append(out, v)
		}
	}
	return out, nil
}

// SimulateMsgDelegateTreasury stakes a small share of a group treasury with
// a bonded validator no member of the group operates, under the treasury
// stake cap, through a proposal of that group.
func SimulateMsgDelegateTreasury(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgDelegateTreasury{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			if e.k.TreasuryStakingKeeper() == nil {
				return proposal{}, "staking keeper not wired", nil
			}
			denom := e.k.BondDenom(e.ctx)
			_, _, headroom, err := e.k.TreasuryStake(e.ctx)
			if err != nil {
				return proposal{}, "", err
			}
			if !headroom.IsPositive() {
				return proposal{}, "the treasuries are at the stake cap", nil
			}
			groups, err := e.groups(func(string, types.Group) bool { return true })
			if err != nil {
				return proposal{}, "", err
			}
			var funded []string
			for _, g := range groups {
				addr, err := e.ak.AddressCodec().StringToBytes(g.g.PolicyAddress)
				if err != nil {
					continue
				}
				if e.bk.SpendableCoins(e.ctx, addr).AmountOf(denom).GTE(math.NewInt(20)) {
					funded = append(funded, g.g.PolicyAddress)
				}
			}
			policy, proposer, reason, err := e.pickCarrier(funded, sdk.MsgTypeURL(label))
			if err != nil || reason != "" {
				return proposal{}, reason, err
			}
			b, _, err := e.policyBody(policy)
			if err != nil {
				return proposal{}, "", err
			}
			vals, err := e.outsideValidators(b.name, "")
			if err != nil {
				return proposal{}, "", err
			}
			val, ok := pick(e.r, vals)
			if !ok {
				return proposal{}, "every bonded validator is operated by a member of the group", nil
			}
			addr, err := e.ak.AddressCodec().StringToBytes(policy)
			if err != nil {
				return proposal{}, "", err
			}
			maxAmt := math.MinInt(e.bk.SpendableCoins(e.ctx, addr).AmountOf(denom).QuoRaw(20), headroom)
			maxAmt = math.MinInt(maxAmt, maxSimTreasuryStake)
			amt, err := simtypes.RandPositiveInt(e.r, maxAmt)
			if err != nil {
				return proposal{}, "treasury too small to stake from", nil
			}
			return proposal{policy: policy, proposer: proposer, inner: &types.MsgDelegateTreasury{
				Authority:        policy,
				ValidatorAddress: val.OperatorAddress,
				Amount:           sdk.NewCoin(denom, amt),
			}}, "", nil
		})
	})
}

// pickStakedCarrier picks a treasury delegation whose group can carry a
// proposal with a message of typeURL.
func (e *simEnv) pickStakedCarrier(typeURL string) (treasuryDelegation, simtypes.Account, string, error) {
	delegations, err := e.treasuryDelegations()
	if err != nil {
		return treasuryDelegation{}, simtypes.Account{}, "", err
	}
	reason := "no group treasury is staked"
	for _, i := range e.r.Perm(len(delegations)) {
		_, proposer, why, err := e.carrier(delegations[i].policy, typeURL)
		if err != nil {
			return treasuryDelegation{}, simtypes.Account{}, "", err
		}
		if why == "" {
			return delegations[i], proposer, "", nil
		}
		reason = why
	}
	return treasuryDelegation{}, simtypes.Account{}, reason, nil
}

// SimulateMsgUndelegateTreasury starts unbonding part or all of one treasury
// delegation, through a proposal of its group.
func SimulateMsgUndelegateTreasury(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUndelegateTreasury{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			d, proposer, reason, err := e.pickStakedCarrier(sdk.MsgTypeURL(label))
			if err != nil || reason != "" {
				return proposal{}, reason, err
			}
			amt, err := simtypes.RandPositiveInt(e.r, d.tokens)
			if err != nil {
				return proposal{}, "delegation too small to undelegate from", nil
			}
			return proposal{policy: d.policy, proposer: proposer, inner: &types.MsgUndelegateTreasury{
				Authority:        d.policy,
				ValidatorAddress: d.validator,
				Amount:           sdk.NewCoin(e.k.BondDenom(e.ctx), amt),
			}}, "", nil
		})
	})
}

// SimulateMsgRedelegateTreasury moves part or all of one treasury delegation
// to another bonded validator no member of the group operates, through a
// proposal of its group.
func SimulateMsgRedelegateTreasury(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRedelegateTreasury{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			d, proposer, reason, err := e.pickStakedCarrier(sdk.MsgTypeURL(label))
			if err != nil || reason != "" {
				return proposal{}, reason, err
			}
			b, _, err := e.policyBody(d.policy)
			if err != nil {
				return proposal{}, "", err
			}
			vals, err := e.outsideValidators(b.name, d.validator)
			if err != nil {
				return proposal{}, "", err
			}
			dst, ok := pick(e.r, vals)
			if !ok {
				return proposal{}, "no other bonded validator outside the group's membership", nil
			}
			amt, err := simtypes.RandPositiveInt(e.r, d.tokens)
			if err != nil {
				return proposal{}, "delegation too small to redelegate from", nil
			}
			return proposal{policy: d.policy, proposer: proposer, inner: &types.MsgRedelegateTreasury{
				Authority:           d.policy,
				SrcValidatorAddress: d.validator,
				DstValidatorAddress: dst.OperatorAddress,
				Amount:              sdk.NewCoin(e.k.BondDenom(e.ctx), amt),
			}}, "", nil
		})
	})
}

// SimulateMsgWithdrawTreasuryRewards has a random live sim account pay the
// pending staking rewards of a staked group treasury into that treasury.
func SimulateMsgWithdrawTreasuryRewards(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgWithdrawTreasuryRewards{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		delegations, err := e.treasuryDelegations()
		if err != nil {
			return noOpErr(label, err)
		}
		d, ok := pick(e.r, delegations)
		if !ok {
			return noOp(label, "no group treasury is staked")
		}
		creator, ok := pick(e.r, e.liveAccounts())
		if !ok {
			return noOp(label, "no live sim account")
		}
		return e.submit(creator, &types.MsgWithdrawTreasuryRewards{
			Creator:          creator.Address.String(),
			PolicyAddress:    d.policy,
			ValidatorAddress: d.validator,
		})
	})
}
