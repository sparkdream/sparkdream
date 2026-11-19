package keeper

import (
	"context"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"sparkdream/x/commons/types"
)

// Treasury staking: a council or committee delegates SPARK from its treasury
// (its standard policy address) to validators through the x/commons wrapper
// messages MsgDelegateTreasury, MsgUndelegateTreasury, MsgRedelegateTreasury
// and MsgWithdrawTreasuryRewards (docs/x-commons-spec.md, "Treasury
// staking"). The wrappers add the group's gates and then run the SDK staking
// and distribution messages through the router as the policy address, so the
// stake is an ordinary delegation and every staking rule still applies.
//
// The group treasuries hold most of the supply, so three rules bound what
// their stake can do:
//
//   - all treasuries together may not delegate above TreasuryStakeCapBps of
//     total bonded stake (checked when a delegation is made);
//   - treasury stake never votes in x/gov: the gov tally leaves it out
//     (app/gov_tally.go reads TreasuryAddresses);
//   - a group may not delegate to a validator operated by one of its
//     members or by a member of a group above it (its parent, and so on).
//     Stake that comes to break the rule because the membership changed is
//     unbonded when the change is made.
//
// A delegation also counts against the group's max_spend_per_epoch like a
// spend: it locks the funds for the unbonding period and exposes them to
// slashing, so the parent's rate limit bounds how fast that can happen.
//
// The raw staking messages are in types.ForbiddenMessages, which is checked
// when a permission is granted and again when a proposal is submitted and
// executed, so a policy can only stake through the wrappers.
//
// No stake ledger is kept: the staked amounts are read from x/staking for
// the treasury addresses (the TreasuryStakers index of live groups that
// have delegated, plus the deleted groups still in TreasuryWindDowns), so
// slashing can never make a ledger drift.

const (
	// TreasuryStakeCapBps caps the SPARK all group treasuries have delegated
	// together, in basis points of total bonded stake (2000 = 20%). It keeps
	// the treasuries well below the third of the stake that can halt the
	// chain. A compiled constant: only a chain upgrade changes it.
	//
	// The cap is checked when a delegation is made. Treasury stake can end
	// up above it afterwards if other delegators unbond; nothing is unbonded
	// by force, and no treasury can delegate more until the share is back
	// under the cap.
	TreasuryStakeCapBps = 2000

	// MaxTreasuryValidatorsPerGroup bounds the validators one treasury is
	// staked with, and so the work of every walk over treasury delegations
	// (the cap check, the gov tally, the wind-down of a deleted group).
	// Delegations worth less than one base unit (what x/staking can leave
	// behind after a slash) are not counted: they cannot be undelegated by
	// amount, so counting them would use up a slot for good.
	MaxTreasuryValidatorsPerGroup = 10

	// maxGroupAncestors bounds the walk up the parent chain of a group. The
	// hierarchy is three levels deep (council, committee, sub-group).
	maxGroupAncestors = 8

	// MaxTreasuryWindDownsPerBlock caps the wind-down entries one EndBlock
	// processes.
	MaxTreasuryWindDownsPerBlock = 5

	// treasuryWindDownRetrySeconds is how long the EndBlocker waits before it
	// tries again to undelegate stake of a deleted group that x/staking
	// refused to release (for example, too many unbonding entries with one
	// validator).
	treasuryWindDownRetrySeconds = 3600

	bpsDenominator = 10_000
)

// treasuryGroup resolves authority to the group whose treasury it is.
// Only the standard policy address holds the treasury; a veto policy
// address is refused.
func (k Keeper) treasuryGroup(ctx context.Context, authority string) (string, types.Group, error) {
	name, group, found := k.GetGroupByPolicy(ctx, authority)
	if !found || group.PolicyAddress != authority {
		return "", types.Group{}, errorsmod.Wrapf(types.ErrNotTreasury,
			"%s is not the standard policy address of a registered group", authority)
	}
	return name, group, nil
}

// groupAndAncestors returns the names of a group and of the groups above
// it: its parent, the parent's parent, and so on. The member-validator rule
// reads all of them, because the members of a parent council decide who
// sits on its committees and can veto or delete them.
func (k Keeper) groupAndAncestors(ctx context.Context, name string, group types.Group) []string {
	names := []string{name}
	seen := map[string]bool{name: true}
	cur := group
	for i := 0; i < maxGroupAncestors && cur.ParentPolicyAddress != ""; i++ {
		parentName, parent, found := k.GetGroupByPolicy(ctx, cur.ParentPolicyAddress)
		if !found || seen[parentName] {
			break
		}
		names = append(names, parentName)
		seen[parentName] = true
		cur = parent
	}
	return names
}

// validatorMemberConflict reports the member that operates validator and the
// group (one of groups) that member sits in, or "" when there is none.
//
// It compares the validator's operator account with the groups' members,
// directly and through the x/rep alias contract (a member who migrated keys
// is still the same member). It cannot see a member who runs a validator
// under an unrelated key; the proposal vote and the stake cap are what bound
// that case.
func (k Keeper) validatorMemberConflict(ctx context.Context, sk types.StakingKeeper, groups []string, validator string) (member, group string, err error) {
	valBz, err := sk.ValidatorAddressCodec().StringToBytes(validator)
	if err != nil {
		return "", "", errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid validator address: %s", err)
	}
	operator, err := k.addressCodec.BytesToString(valBz)
	if err != nil {
		return "", "", errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid validator operator: %s", err)
	}
	candidates := []string{operator}
	if k.late.repKeeper != nil {
		if canonical := k.late.repKeeper.Canonical(ctx, operator); canonical != "" && canonical != operator {
			candidates = append(candidates, canonical)
		}
	}
	for _, g := range groups {
		for _, addr := range candidates {
			isMember, err := k.HasMember(ctx, g, addr)
			if err != nil {
				return "", "", err
			}
			if isMember {
				return addr, g, nil
			}
		}
	}
	return "", "", nil
}

// stakingKeeper returns the late-wired staking keeper or an error.
func (k Keeper) stakingKeeper() (types.StakingKeeper, error) {
	if k.late.stakingKeeper == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "staking keeper not wired into commons")
	}
	return k.late.stakingKeeper, nil
}

// TreasuryStakingKeeper returns the late-wired staking keeper, nil when
// unwired. The simulation reads validators and delegations through it.
func (k Keeper) TreasuryStakingKeeper() types.StakingKeeper {
	return k.late.stakingKeeper
}

// validateStakeAmount requires a positive amount in the bond denom.
func (k Keeper) validateStakeAmount(ctx context.Context, amount sdk.Coin) error {
	if !amount.IsValid() || !amount.Amount.IsPositive() {
		return errorsmod.Wrapf(types.ErrInvalidTreasuryStake, "amount %s must be positive", amount)
	}
	if bond := k.BondDenom(ctx); amount.Denom != bond {
		return errorsmod.Wrapf(types.ErrInvalidTreasuryStake, "amount denom %s is not the bond denom %s", amount.Denom, bond)
	}
	return nil
}

// checkTreasuryValidator checks a validator a group wants to delegate (or
// redelegate) to: it exists, it is bonded, and no member of the group or of
// a group above it operates it.
//
// The validator must be bonded because the stake cap compares treasury
// stake with total bonded stake and assumes a new delegation adds to both;
// stake sent to an unbonded or jailed validator would secure nothing and
// still use up the headroom of every other treasury.
func (k Keeper) checkTreasuryValidator(ctx context.Context, groupName string, group types.Group, validator string) error {
	sk, err := k.stakingKeeper()
	if err != nil {
		return err
	}
	valBz, err := sk.ValidatorAddressCodec().StringToBytes(validator)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid validator address: %s", err)
	}
	val, err := sk.GetValidator(ctx, valBz)
	if err != nil {
		return errorsmod.Wrapf(stakingtypes.ErrNoValidatorFound, "validator %s", validator)
	}
	if !val.IsBonded() {
		return errorsmod.Wrapf(types.ErrInvalidTreasuryStake,
			"validator %s is not bonded (status %s)", validator, val.GetStatus())
	}
	member, in, err := k.validatorMemberConflict(ctx, sk, k.groupAndAncestors(ctx, groupName, group), validator)
	if err != nil {
		return err
	}
	if member != "" {
		return errorsmod.Wrapf(types.ErrValidatorOperatedByMember,
			"validator %s is operated by %s, a member of %s", validator, member, in)
	}
	return nil
}

// checkTreasuryPositions checks a treasury's delegations after a delegation
// or redelegation was routed (the tx reverts on an error): the treasury is
// staked with at most MaxTreasuryValidatorsPerGroup validators, and, with
// memberRule, none of them is operated by a member of the group or of a
// group above it.
//
// Counting after the fact is what lets a treasury at the limit move a whole
// delegation to a new validator. The member rule is re-read here because the
// membership can change after a delegation is made; a group whose stake
// breaks the rule must undelegate or redelegate it before it stakes more
// (redelegations pass memberRule = false so they can do that).
func (k Keeper) checkTreasuryPositions(ctx context.Context, groupName string, group types.Group, memberRule bool) error {
	sk, err := k.stakingKeeper()
	if err != nil {
		return err
	}
	policyBz, err := k.addressCodec.StringToBytes(group.PolicyAddress)
	if err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid policy address")
	}
	delegations, err := sk.GetDelegatorDelegations(ctx, policyBz, ^uint16(0))
	if err != nil {
		return err
	}
	var groups []string
	if memberRule {
		groups = k.groupAndAncestors(ctx, groupName, group)
	}
	vals := map[string]stakingtypes.Validator{}
	count := 0
	for _, d := range delegations {
		val, ok := cachedValidator(ctx, sk, d.ValidatorAddress, vals)
		if !ok {
			return errorsmod.Wrapf(stakingtypes.ErrNoValidatorFound, "validator %s", d.ValidatorAddress)
		}
		if val.DelegatorShares.IsZero() || !val.TokensFromShares(d.Shares).TruncateInt().IsPositive() {
			continue
		}
		count++
		if !memberRule {
			continue
		}
		member, in, err := k.validatorMemberConflict(ctx, sk, groups, d.ValidatorAddress)
		if err != nil {
			return err
		}
		if member != "" {
			return errorsmod.Wrapf(types.ErrValidatorOperatedByMember,
				"%s is staked with validator %s, operated by %s, a member of %s; undelegate or redelegate that stake first",
				groupName, d.ValidatorAddress, member, in)
		}
	}
	if count > MaxTreasuryValidatorsPerGroup {
		return errorsmod.Wrapf(types.ErrTooManyTreasuryValidators,
			"%s would delegate to %d validators (max %d)", groupName, count, MaxTreasuryValidatorsPerGroup)
	}
	return nil
}

// unbondMemberValidatorStake enforces the member-validator rule after the
// membership of changed was edited: every treasury of changed or of a group
// below it has the stake it holds with a validator one of the members now
// operates undelegated in full.
//
// The membership change never waits for the stake (a group could otherwise
// keep a validator operator out by delegating to them). An undelegation
// x/staking refuses (too many unbonding entries) is logged and reported in
// an event; checkTreasuryPositions then keeps that treasury from staking
// more until the group has moved the stake itself.
func (k Keeper) unbondMemberValidatorStake(ctx sdk.Context, changed string) error {
	sk := k.late.stakingKeeper
	if sk == nil {
		return nil
	}
	var stakers []string
	if err := k.TreasuryStakers.Walk(ctx, nil, func(policy string) (bool, error) {
		stakers = append(stakers, policy)
		return false, nil
	}); err != nil {
		return err
	}
	vals := map[string]stakingtypes.Validator{}
	for _, policy := range stakers {
		name, group, err := k.treasuryGroup(ctx, policy)
		if err != nil {
			continue
		}
		groups := k.groupAndAncestors(ctx, name, group)
		if !slices.Contains(groups, changed) {
			continue
		}
		policyBz, err := k.addressCodec.StringToBytes(policy)
		if err != nil {
			continue
		}
		delegations, err := sk.GetDelegatorDelegations(ctx, policyBz, ^uint16(0))
		if err != nil {
			return err
		}
		for _, d := range delegations {
			member, in, err := k.validatorMemberConflict(ctx, sk, groups, d.ValidatorAddress)
			if err != nil {
				return err
			}
			if member == "" {
				continue
			}
			event := sdk.NewEvent(
				"treasury_member_validator_unbonded",
				sdk.NewAttribute("group", name),
				sdk.NewAttribute("policy_address", policy),
				sdk.NewAttribute("validator", d.ValidatorAddress),
				sdk.NewAttribute("member", member),
				sdk.NewAttribute("member_of", in),
			)
			if err := k.undelegateAll(ctx, sk, policy, d, vals); err != nil {
				ctx.Logger().Error("treasury stake with a member's validator could not be unbonded",
					"module", "x/commons", "group", name, "validator", d.ValidatorAddress, "err", err)
				event = event.AppendAttributes(sdk.NewAttribute("refused", err.Error()))
			}
			ctx.EventManager().EmitEvent(event)
		}
	}
	return nil
}

// TreasuryAddresses lists every address whose delegations count as treasury
// stake: the standard policy address of each live group that has delegated
// (TreasuryStakers), and the policy address of each deleted group whose
// stake is still winding down. The x/gov tally (app/gov_tally.go) and the
// stake cap both read it.
//
// The index is complete because a policy can only delegate through
// MsgDelegateTreasury, which writes it; TreasuryStakersInvariant checks it
// against x/staking.
func (k Keeper) TreasuryAddresses(ctx context.Context) ([]string, error) {
	var addrs []string
	if err := k.TreasuryStakers.Walk(ctx, nil, func(policy string) (bool, error) {
		addrs = append(addrs, policy)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TreasuryWindDowns.Walk(ctx, nil, func(key collections.Pair[int64, string], _ string) (bool, error) {
		addrs = append(addrs, key.K2())
		return false, nil
	}); err != nil {
		return nil, err
	}
	return addrs, nil
}

// isWindingDown reports whether policy is a deleted group's treasury still
// in the wind-down queue. Only a deleted policy can be in the queue, so any
// other address is answered from the DeletedPolicies index without a scan;
// the queue itself is small (deleted groups with stake).
func (k Keeper) isWindingDown(ctx context.Context, policy string) (bool, error) {
	deleted, err := k.DeletedPolicies.Has(ctx, policy)
	if err != nil || !deleted {
		return false, err
	}
	found := false
	err = k.TreasuryWindDowns.Walk(ctx, nil, func(key collections.Pair[int64, string], _ string) (bool, error) {
		found = key.K2() == policy
		return found, nil
	})
	return found, err
}

// cachedValidator looks a validator up once per walk.
func cachedValidator(ctx context.Context, sk types.StakingKeeper, valAddr string, vals map[string]stakingtypes.Validator) (stakingtypes.Validator, bool) {
	if val, ok := vals[valAddr]; ok {
		return val, true
	}
	valBz, err := sk.ValidatorAddressCodec().StringToBytes(valAddr)
	if err != nil {
		return stakingtypes.Validator{}, false
	}
	val, err := sk.GetValidator(ctx, valBz)
	if err != nil {
		return stakingtypes.Validator{}, false
	}
	vals[valAddr] = val
	return val, true
}

// stakedBy returns the SPARK addr has delegated, at the validators' current
// exchange rates. vals caches validator lookups across calls.
func (k Keeper) stakedBy(ctx context.Context, sk types.StakingKeeper, addr string, vals map[string]stakingtypes.Validator) (math.Int, error) {
	total := math.ZeroInt()
	bz, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		// A malformed stored address holds no delegation.
		return total, nil
	}
	delegations, err := sk.GetDelegatorDelegations(ctx, bz, ^uint16(0))
	if err != nil {
		return total, err
	}
	for _, d := range delegations {
		val, ok := cachedValidator(ctx, sk, d.ValidatorAddress, vals)
		if !ok || val.DelegatorShares.IsZero() {
			continue
		}
		total = total.Add(val.TokensFromShares(d.Shares).TruncateInt())
	}
	return total, nil
}

// TreasuryStake returns what all treasuries have delegated together, the
// chain's total bonded stake, and the headroom: the largest amount x a
// treasury could delegate now with
//
//	(staked + x) / (bonded + x) <= TreasuryStakeCapBps / 10000.
//
// x is added to both sides because a treasury may only delegate to a bonded
// validator (checkTreasuryValidator). staked counts treasury stake with any
// validator, also one that has left the active set since: that stake comes
// back into the bonded total when the validator does, so leaving it out
// would let the treasuries pass the cap through a validator that drops out
// and returns.
func (k Keeper) TreasuryStake(ctx context.Context) (staked, bonded, headroom math.Int, err error) {
	staked, bonded, headroom = math.ZeroInt(), math.ZeroInt(), math.ZeroInt()
	sk, err := k.stakingKeeper()
	if err != nil {
		return staked, bonded, headroom, err
	}
	addrs, err := k.TreasuryAddresses(ctx)
	if err != nil {
		return staked, bonded, headroom, err
	}
	vals := map[string]stakingtypes.Validator{}
	for _, addr := range addrs {
		amt, err := k.stakedBy(ctx, sk, addr, vals)
		if err != nil {
			return staked, bonded, headroom, err
		}
		staked = staked.Add(amt)
	}
	bonded, err = sk.TotalBondedTokens(ctx)
	if err != nil {
		return staked, bonded, headroom, err
	}
	// x * (10000 - cap) <= cap * bonded - 10000 * staked
	room := bonded.MulRaw(TreasuryStakeCapBps).Sub(staked.MulRaw(bpsDenominator))
	if room.IsPositive() {
		headroom = room.QuoRaw(bpsDenominator - TreasuryStakeCapBps)
	}
	return staked, bonded, headroom, nil
}

// routeAs runs msg through the message router, as a proposal would, and
// forwards its events. The handler is called directly, so the message's
// declared signer is not checked: callers have authorized the action.
func (k Keeper) routeAs(ctx sdk.Context, msg sdk.Msg) (*sdk.Result, error) {
	if k.late.router == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "message router not wired into commons")
	}
	handler := k.late.router.Handler(msg)
	if handler == nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnknownRequest, "no handler for %s", sdk.MsgTypeURL(msg))
	}
	res, err := handler(ctx, msg)
	if err != nil {
		return nil, err
	}
	if res != nil {
		ctx.EventManager().EmitEvents(res.GetEvents())
	}
	return res, nil
}

// routedResponse decodes the single response of a routed message into out.
func (k Keeper) routedResponse(res *sdk.Result, out sdk.Msg) error {
	if res == nil || len(res.MsgResponses) != 1 {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "routed message returned no response")
	}
	return k.cdc.Unmarshal(res.MsgResponses[0].Value, out)
}

// startTreasuryWindDown queues a deleted group's treasury for unwinding when
// it holds any delegation or unbonding entry. Called by MsgDeleteGroup, which
// must never fail because of a group's stake: a group could otherwise shield
// itself from deletion by delegating. The EndBlocker does the work.
func (k Keeper) startTreasuryWindDown(ctx context.Context, policy, heir string) error {
	sk := k.late.stakingKeeper
	if sk == nil {
		return nil
	}
	bz, err := k.addressCodec.StringToBytes(policy)
	if err != nil {
		return nil
	}
	delegations, err := sk.GetDelegatorDelegations(ctx, bz, 1)
	if err != nil {
		return err
	}
	unbonding, err := sk.GetUnbondingDelegations(ctx, bz, 1)
	if err != nil {
		return err
	}
	if len(delegations) == 0 && len(unbonding) == 0 {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.TreasuryWindDowns.Set(ctx, collections.Join(sdkCtx.BlockTime().Unix(), policy), heir); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"treasury_wind_down_started",
		sdk.NewAttribute("policy_address", policy),
		sdk.NewAttribute("heir", heir),
	))
	return nil
}

// reheirTreasuryWindDowns re-points the wind-downs that were to be swept to
// a group that is now being deleted itself: they go to newHeir, the deleted
// group's own parent ("" when that is not a group). Without this the stake
// would mature after its heir is gone and stay at a keyless address.
func (k Keeper) reheirTreasuryWindDowns(ctx context.Context, former []string, newHeir string) error {
	var keys []collections.Pair[int64, string]
	if err := k.TreasuryWindDowns.Walk(ctx, nil, func(key collections.Pair[int64, string], heir string) (bool, error) {
		if heir != "" && slices.Contains(former, heir) {
			keys = append(keys, key)
		}
		return false, nil
	}); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, key := range keys {
		if err := k.TreasuryWindDowns.Set(ctx, key, newHeir); err != nil {
			return err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"treasury_wind_down_heir_changed",
			sdk.NewAttribute("policy_address", key.K2()),
			sdk.NewAttribute("heir", newHeir),
		))
	}
	return nil
}

// EndBlockTreasuryWindDowns advances the wind-down of deleted groups'
// treasury stake: undelegate what is still staked, wait for the unbonding
// entries to mature, then sweep the balance to the heir. It never halts the
// chain: a failing entry is logged and tried again later.
func (k Keeper) EndBlockTreasuryWindDowns(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	rng := new(collections.Range[collections.Pair[int64, string]]).
		EndExclusive(collections.Join(now+1, ""))
	type entry struct {
		key  collections.Pair[int64, string]
		heir string
	}
	var due []entry
	if err := k.TreasuryWindDowns.Walk(ctx, rng, func(key collections.Pair[int64, string], heir string) (bool, error) {
		due = append(due, entry{key: key, heir: heir})
		return len(due) >= MaxTreasuryWindDownsPerBlock, nil
	}); err != nil {
		return err
	}

	for _, e := range due {
		if err := k.TreasuryWindDowns.Remove(ctx, e.key); err != nil {
			return err
		}
		policy := e.key.K2()
		next, done := k.windDownTreasury(sdkCtx, policy, e.heir)
		if done {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"treasury_wind_down_finished",
				sdk.NewAttribute("policy_address", policy),
				sdk.NewAttribute("heir", e.heir),
			))
			continue
		}
		if next <= now {
			next = now + 1
		}
		if err := k.TreasuryWindDowns.Set(ctx, collections.Join(next, policy), e.heir); err != nil {
			return err
		}
	}
	return nil
}

// windDownTreasury does one step for a deleted group's treasury. It returns
// done when nothing is staked or unbonding any more (the balance has then
// been swept to the heir), else the time of the next step.
func (k Keeper) windDownTreasury(ctx sdk.Context, policy, heir string) (next int64, done bool) {
	logger := ctx.Logger().With("module", "x/commons", "treasury_wind_down", policy)
	now := ctx.BlockTime().Unix()
	retry := now + treasuryWindDownRetrySeconds

	policyBz, err := k.addressCodec.StringToBytes(policy)
	if err != nil {
		// An address that does not decode holds no delegation; drop it.
		return 0, true
	}
	sk := k.late.stakingKeeper
	if sk == nil {
		// Never in the app (app.go wires it). Keep the entry: dropping it
		// would take the address out of TreasuryAddresses with its stake
		// still in place.
		logger.Error("staking keeper not wired; retrying later")
		return retry, false
	}

	// 1. Undelegate whatever is still staked. Each undelegation runs in its
	// own cache context so a refusal leaves no partial write.
	delegations, err := sk.GetDelegatorDelegations(ctx, policyBz, ^uint16(0))
	if err != nil {
		logger.Error("reading delegations failed; retrying later", "err", err)
		return retry, false
	}
	stuck := false
	vals := map[string]stakingtypes.Validator{}
	for _, d := range delegations {
		// Every delegation must go before the entry can finish: one left
		// behind would drop out of TreasuryAddresses with the entry, vote in
		// x/gov and escape the stake cap, with nobody able to undelegate it.
		if err := k.undelegateAll(ctx, sk, policy, d, vals); err != nil {
			logger.Error("undelegation refused; retrying later", "validator", d.ValidatorAddress, "err", err)
			stuck = true
		}
	}

	// 2. Wait for the unbonding entries to mature.
	unbonding, err := sk.GetUnbondingDelegations(ctx, policyBz, ^uint16(0))
	if err != nil {
		logger.Error("reading unbonding delegations failed; retrying later", "err", err)
		return retry, false
	}
	var matures int64
	for _, ubd := range unbonding {
		for _, entry := range ubd.Entries {
			if t := entry.CompletionTime.Unix() + 1; t > matures {
				matures = t
			}
		}
	}
	switch {
	case matures > 0 && stuck:
		return min(matures, retry), false
	case matures > 0:
		return matures, false
	case stuck:
		return retry, false
	}

	// 3. Nothing staked, nothing unbonding: sweep to the heir, if it is
	// still a group. Without one the funds stay where they are, like the
	// rest of the treasury of a group whose parent was x/gov.
	if heir != "" && k.IsGroupPolicyAddress(ctx, heir) {
		cacheCtx, write := ctx.CacheContext()
		if err := k.sweepPolicyBalance(cacheCtx, []string{policy}, heir); err != nil {
			logger.Error("sweeping the unbonded treasury failed; retrying later", "err", err)
			return retry, false
		}
		write()
	}
	return 0, true
}

// undelegateAll removes one whole delegation of policy. A delegation worth
// at least one base unit goes through MsgUndelegate; what is worth less
// (x/staking can leave such dust after a slash) cannot be undelegated by
// amount and is unbonded by its shares through the staking keeper.
func (k Keeper) undelegateAll(ctx sdk.Context, sk types.StakingKeeper, policy string, d stakingtypes.Delegation, vals map[string]stakingtypes.Validator) error {
	val, ok := cachedValidator(ctx, sk, d.ValidatorAddress, vals)
	if !ok {
		return errorsmod.Wrapf(stakingtypes.ErrNoValidatorFound, "validator %s", d.ValidatorAddress)
	}
	if !val.DelegatorShares.IsZero() {
		if tokens := val.TokensFromShares(d.Shares).TruncateInt(); tokens.IsPositive() {
			return k.undelegateInCache(ctx, policy, d.ValidatorAddress, sdk.NewCoin(k.BondDenom(ctx), tokens))
		}
	}
	return k.unbondSharesInCache(ctx, sk, policy, d)
}

// unbondSharesInCache unbonds a whole delegation by its shares, in a cache
// context, containing errors and panics.
func (k Keeper) unbondSharesInCache(ctx sdk.Context, sk types.StakingKeeper, policy string, d stakingtypes.Delegation) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	policyBz, err := k.addressCodec.StringToBytes(policy)
	if err != nil {
		return err
	}
	valBz, err := sk.ValidatorAddressCodec().StringToBytes(d.ValidatorAddress)
	if err != nil {
		return err
	}
	cacheCtx, write := ctx.CacheContext()
	if _, _, err := sk.Undelegate(cacheCtx, policyBz, valBz, d.Shares); err != nil {
		return err
	}
	write()
	return nil
}

// undelegateInCache routes one MsgUndelegate for policy in a cache context,
// containing handler errors and panics.
func (k Keeper) undelegateInCache(ctx sdk.Context, policy, validator string, amount sdk.Coin) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	cacheCtx, write := ctx.CacheContext()
	if _, err := k.routeAs(cacheCtx, &stakingtypes.MsgUndelegate{
		DelegatorAddress: policy,
		ValidatorAddress: validator,
		Amount:           amount,
	}); err != nil {
		return err
	}
	write()
	return nil
}
