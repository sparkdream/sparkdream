package keeper_test

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// fakeStaking is an x/staking double: validators at a 1:1 exchange rate,
// delegations and unbonding entries per delegator. It is also the message
// router of the test, handling the SDK messages the treasury wrappers route.
type fakeStaking struct {
	bank *mockBankKeeperCommons
	// outside is bonded stake that belongs to nobody the test tracks.
	outside math.Int
	vals    map[string]stakingtypes.Validator
	dels    map[string]map[string]math.Int
	ubds    map[string][]fakeUnbonding
	rewards map[string]math.Int
	// refuse makes undelegations from these validators fail.
	refuse map[string]bool
	// routed records the type URLs of the messages that reached the router.
	routed []string
	// unbondedShares records the validators whose delegation was unbonded
	// by shares (Undelegate), not through the router.
	unbondedShares []string
}

type fakeUnbonding struct {
	validator  string
	amount     math.Int
	completion time.Time
}

const fakeUnbondingTime = 21 * 24 * time.Hour

func newFakeStaking(bank *mockBankKeeperCommons, outside int64) *fakeStaking {
	return &fakeStaking{
		bank:    bank,
		outside: math.NewInt(outside),
		vals:    map[string]stakingtypes.Validator{},
		dels:    map[string]map[string]math.Int{},
		ubds:    map[string][]fakeUnbonding{},
		rewards: map[string]math.Int{},
		refuse:  map[string]bool{},
	}
}

func valoperOf(operator string) string {
	// The keeper under test uses the "cosmos" prefix whatever the global
	// bech32 config another test has set.
	bz, err := sdk.GetFromBech32(operator, "cosmos")
	if err != nil {
		panic(err)
	}
	return sdk.MustBech32ifyAddressBytes("cosmosvaloper", bz)
}

// bankKey is the key the bank double files a "cosmos"-prefixed address
// under: it indexes balances by AccAddress.String(), which follows the
// global bech32 prefix (another test in the package changes it).
func bankKey(addr string) string {
	bz, err := sdk.GetFromBech32(addr, "cosmos")
	if err != nil {
		panic(err)
	}
	return sdk.AccAddress(bz).String()
}

// addValidator registers a bonded validator operated by operator.
func (f *fakeStaking) addValidator(operator string) string {
	v := valoperOf(operator)
	f.vals[v] = stakingtypes.Validator{
		OperatorAddress: v,
		Status:          stakingtypes.Bonded,
		Tokens:          math.ZeroInt(),
		DelegatorShares: math.LegacyZeroDec(),
	}
	return v
}

func (f *fakeStaking) delegated(delegator, validator string) math.Int {
	if amt, ok := f.dels[delegator][validator]; ok {
		return amt
	}
	return math.ZeroInt()
}

func (f *fakeStaking) move(delegator, validator string, delta math.Int) {
	if f.dels[delegator] == nil {
		f.dels[delegator] = map[string]math.Int{}
	}
	next := f.delegated(delegator, validator).Add(delta)
	if next.IsZero() {
		delete(f.dels[delegator], validator)
	} else {
		f.dels[delegator][validator] = next
	}
	val := f.vals[validator]
	val.Tokens = val.Tokens.Add(delta)
	val.DelegatorShares = math.LegacyNewDecFromInt(val.Tokens)
	f.vals[validator] = val
}

// mature pays every unbonding entry due at now into the delegator's balance.
func (f *fakeStaking) mature(now time.Time) {
	for del, entries := range f.ubds {
		var keep []fakeUnbonding
		for _, e := range entries {
			if e.completion.After(now) {
				keep = append(keep, e)
				continue
			}
			f.bank.balance[bankKey(del)] = f.bank.balance[bankKey(del)].Add(sdk.NewCoin("uspark", e.amount))
		}
		if len(keep) == 0 {
			delete(f.ubds, del)
		} else {
			f.ubds[del] = keep
		}
	}
}

// --- types.StakingKeeper ---

func (f *fakeStaking) ValidatorAddressCodec() address.Codec {
	return addresscodec.NewBech32Codec("cosmosvaloper")
}

func (f *fakeStaking) GetValidator(_ context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	v, ok := f.vals[sdk.MustBech32ifyAddressBytes("cosmosvaloper", addr)]
	if !ok {
		return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
	}
	return v, nil
}

func (f *fakeStaking) GetBondedValidatorsByPower(context.Context) ([]stakingtypes.Validator, error) {
	var out []stakingtypes.Validator
	for _, v := range f.vals {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OperatorAddress < out[j].OperatorAddress })
	return out, nil
}

func (f *fakeStaking) GetDelegatorDelegations(_ context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error) {
	del := sdk.MustBech32ifyAddressBytes("cosmos", delegator)
	var vals []string
	for v := range f.dels[del] {
		vals = append(vals, v)
	}
	sort.Strings(vals)
	var out []stakingtypes.Delegation
	for _, v := range vals {
		if len(out) >= int(maxRetrieve) {
			break
		}
		out = append(out, stakingtypes.Delegation{
			DelegatorAddress: del,
			ValidatorAddress: v,
			Shares:           math.LegacyNewDecFromInt(f.dels[del][v]),
		})
	}
	return out, nil
}

func (f *fakeStaking) GetUnbondingDelegations(_ context.Context, delegator sdk.AccAddress, _ uint16) ([]stakingtypes.UnbondingDelegation, error) {
	del := sdk.MustBech32ifyAddressBytes("cosmos", delegator)
	var out []stakingtypes.UnbondingDelegation
	for _, e := range f.ubds[del] {
		out = append(out, stakingtypes.UnbondingDelegation{
			DelegatorAddress: del,
			ValidatorAddress: e.validator,
			Entries:          []stakingtypes.UnbondingDelegationEntry{{CompletionTime: e.completion, Balance: e.amount}},
		})
	}
	return out, nil
}

func (f *fakeStaking) TotalBondedTokens(context.Context) (math.Int, error) {
	total := f.outside
	for _, v := range f.vals {
		total = total.Add(v.Tokens)
	}
	return total, nil
}

// Undelegate unbonds shares of a delegation (the wind-down's path for a
// delegation worth less than one base unit).
func (f *fakeStaking) Undelegate(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, shares math.LegacyDec) (time.Time, math.Int, error) {
	del := sdk.MustBech32ifyAddressBytes("cosmos", delAddr)
	val := sdk.MustBech32ifyAddressBytes("cosmosvaloper", valAddr)
	if f.refuse[val] || math.LegacyNewDecFromInt(f.delegated(del, val)).LT(shares) {
		return time.Time{}, math.ZeroInt(), errors.New("undelegation refused")
	}
	f.unbondedShares = append(f.unbondedShares, val)
	delete(f.dels[del], val)
	return sdk.UnwrapSDKContext(ctx).BlockTime().Add(fakeUnbondingTime), math.ZeroInt(), nil
}

// revert takes back a delegation the fake recorded for a tx the keeper then
// refused: the fake's state is not in the store, so it does not roll back
// with the tx.
func (f *fakeStaking) revert(delegator, validator string, amount int64) {
	f.move(delegator, validator, math.NewInt(-amount))
	f.bank.balance[bankKey(delegator)] = f.bank.balance[bankKey(delegator)].Add(uspark(amount))
}

// --- baseapp.MessageRouter ---

func (f *fakeStaking) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return nil }

func (f *fakeStaking) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	return func(ctx sdk.Context, req sdk.Msg) (*sdk.Result, error) {
		f.routed = append(f.routed, sdk.MsgTypeURL(req))
		switch m := req.(type) {
		case *stakingtypes.MsgDelegate:
			coins := sdk.NewCoins(m.Amount)
			if !f.bank.balance[bankKey(m.DelegatorAddress)].IsAllGTE(coins) {
				return nil, errors.New("insufficient funds")
			}
			f.bank.balance[bankKey(m.DelegatorAddress)] = f.bank.balance[bankKey(m.DelegatorAddress)].Sub(coins...)
			f.move(m.DelegatorAddress, m.ValidatorAddress, m.Amount.Amount)
			return sdk.WrapServiceResult(ctx, &stakingtypes.MsgDelegateResponse{}, nil)
		case *stakingtypes.MsgUndelegate:
			if f.refuse[m.ValidatorAddress] || f.delegated(m.DelegatorAddress, m.ValidatorAddress).LT(m.Amount.Amount) {
				return nil, errors.New("undelegation refused")
			}
			f.move(m.DelegatorAddress, m.ValidatorAddress, m.Amount.Amount.Neg())
			completion := ctx.BlockTime().Add(fakeUnbondingTime)
			f.ubds[m.DelegatorAddress] = append(f.ubds[m.DelegatorAddress], fakeUnbonding{
				validator: m.ValidatorAddress, amount: m.Amount.Amount, completion: completion,
			})
			return sdk.WrapServiceResult(ctx, &stakingtypes.MsgUndelegateResponse{CompletionTime: completion, Amount: m.Amount}, nil)
		case *stakingtypes.MsgBeginRedelegate:
			if f.delegated(m.DelegatorAddress, m.ValidatorSrcAddress).LT(m.Amount.Amount) {
				return nil, errors.New("redelegation refused")
			}
			f.move(m.DelegatorAddress, m.ValidatorSrcAddress, m.Amount.Amount.Neg())
			f.move(m.DelegatorAddress, m.ValidatorDstAddress, m.Amount.Amount)
			return sdk.WrapServiceResult(ctx, &stakingtypes.MsgBeginRedelegateResponse{CompletionTime: ctx.BlockTime().Add(fakeUnbondingTime)}, nil)
		case *distrtypes.MsgWithdrawDelegatorReward:
			amt, ok := f.rewards[m.DelegatorAddress]
			if !ok {
				return nil, errors.New("no delegation")
			}
			paid := sdk.NewCoins(sdk.NewCoin("uspark", amt))
			f.bank.balance[bankKey(m.DelegatorAddress)] = f.bank.balance[bankKey(m.DelegatorAddress)].Add(paid...)
			delete(f.rewards, m.DelegatorAddress)
			return sdk.WrapServiceResult(ctx, &distrtypes.MsgWithdrawDelegatorRewardResponse{Amount: paid}, nil)
		}
		return nil, errors.New("unexpected message")
	}
}

// treasuryFixture is a commons keeper wired to fakeStaking, with one funded
// group "Stakers" whose only member is `member`.
type treasuryFixture struct {
	k      keeper.Keeper
	ctx    sdk.Context
	ms     types.MsgServer
	bank   *mockBankKeeperCommons
	sk     *fakeStaking
	policy string
	member string
	// val is a validator nobody in the group operates.
	val string
}

func uspark(n int64) sdk.Coin { return sdk.NewInt64Coin("uspark", n) }

func setupTreasury(t *testing.T) *treasuryFixture {
	t.Helper()
	k, ctx, bank := setupCommonsKeeper(t)
	// 1000 bonded by others: the 20% cap leaves 250 of headroom.
	sk := newFakeStaking(bank, 1000)
	k.SetStakingKeeper(sk)
	k.SetRouter(sk)

	f := &treasuryFixture{k: k, ctx: ctx, ms: keeper.NewMsgServerImpl(k), bank: bank, sk: sk}
	f.policy = cosmosAddr("stakers_policy______")
	f.member = cosmosAddr("stakers_member______")
	f.addGroup(t, "Stakers", f.policy, f.member, "")
	f.val = sk.addValidator(cosmosAddr("outside_operator____"))
	return f
}

// addGroup registers a funded group with one member.
func (f *treasuryFixture) addGroup(t *testing.T, name, policy, member, parent string) {
	t.Helper()
	setupProposalState(t, f.k, f.ctx, name, policy, member)
	g, err := f.k.Groups.Get(f.ctx, name)
	require.NoError(t, err)
	g.Index = name
	g.ParentPolicyAddress = parent
	require.NoError(t, f.k.Groups.Set(f.ctx, name, g))
	f.bank.balance[bankKey(policy)] = sdk.NewCoins(uspark(1_000_000))
}

func (f *treasuryFixture) delegate(policy, val string, amount int64) error {
	_, err := f.ms.DelegateTreasury(f.ctx, &types.MsgDelegateTreasury{
		Authority: policy, ValidatorAddress: val, Amount: uspark(amount),
	})
	return err
}

func TestDelegateTreasury(t *testing.T) {
	f := setupTreasury(t)

	require.NoError(t, f.delegate(f.policy, f.val, 100))
	require.Equal(t, math.NewInt(100), f.sk.delegated(f.policy, f.val))
	require.Equal(t, []string{"/cosmos.staking.v1beta1.MsgDelegate"}, f.sk.routed)

	staked, bonded, headroom, err := f.k.TreasuryStake(f.ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100), staked)
	require.Equal(t, math.NewInt(1100), bonded)
	// (100 + x) / (1100 + x) <= 0.2  =>  x <= 150
	require.Equal(t, math.NewInt(150), headroom)

	res, err := keeper.NewQueryServerImpl(f.k).TreasuryStake(f.ctx, &types.QueryTreasuryStakeRequest{})
	require.NoError(t, err)
	require.Equal(t, staked, res.TreasuryStaked)
	require.Equal(t, headroom, res.Headroom)
	require.Equal(t, uint64(keeper.TreasuryStakeCapBps), res.CapBps)
}

func TestDelegateTreasury_InvalidRequests(t *testing.T) {
	f := setupTreasury(t)

	// Not a group at all.
	require.ErrorIs(t, f.delegate(cosmosAddr("some_plain_account__"), f.val, 10), types.ErrNotTreasury)

	// A veto policy address resolves to the group but holds no treasury.
	veto := cosmosAddr("stakers_veto_policy_")
	require.NoError(t, f.k.PolicyToName.Set(f.ctx, veto, "Stakers"))
	require.ErrorIs(t, f.delegate(veto, f.val, 10), types.ErrNotTreasury)

	// Wrong denom, zero amount, unknown validator.
	_, err := f.ms.DelegateTreasury(f.ctx, &types.MsgDelegateTreasury{
		Authority: f.policy, ValidatorAddress: f.val, Amount: sdk.NewInt64Coin("udream", 10),
	})
	require.ErrorIs(t, err, types.ErrInvalidTreasuryStake)
	require.ErrorIs(t, f.delegate(f.policy, f.val, 0), types.ErrInvalidTreasuryStake)
	require.ErrorIs(t, f.delegate(f.policy, valoperOf(cosmosAddr("no_such_validator___")), 10), stakingtypes.ErrNoValidatorFound)

	require.Empty(t, f.sk.routed)
}

// The cap holds across all treasuries together, not per group.
func TestDelegateTreasury_StakeCap(t *testing.T) {
	f := setupTreasury(t)
	other := cosmosAddr("second_policy_______")
	f.addGroup(t, "Second", other, cosmosAddr("second_member_______"), "")

	// 1000 bonded by others: at most 250 of treasury stake (250/1250 = 20%).
	require.ErrorIs(t, f.delegate(f.policy, f.val, 251), types.ErrTreasuryStakeCap)
	require.NoError(t, f.delegate(f.policy, f.val, 200))
	require.ErrorIs(t, f.delegate(other, f.val, 51), types.ErrTreasuryStakeCap)
	require.NoError(t, f.delegate(other, f.val, 50))
	require.ErrorIs(t, f.delegate(f.policy, f.val, 1), types.ErrTreasuryStakeCap)

	// Outside stake leaving does not unbond anything, but nothing more can
	// be delegated while the treasuries sit above the cap.
	f.sk.outside = math.NewInt(100)
	_, _, headroom, err := f.k.TreasuryStake(f.ctx)
	require.NoError(t, err)
	require.True(t, headroom.IsZero())
	require.Equal(t, math.NewInt(200), f.sk.delegated(f.policy, f.val))
}

func TestDelegateTreasury_MemberValidatorRefused(t *testing.T) {
	f := setupTreasury(t)
	memberVal := f.sk.addValidator(f.member)

	require.ErrorIs(t, f.delegate(f.policy, memberVal, 10), types.ErrValidatorOperatedByMember)

	// A validator whose operator key migrated to a member's address is still
	// that member's validator.
	oldKey := cosmosAddr("member_old_key______")
	migratedVal := f.sk.addValidator(oldKey)
	f.k.SetRepKeeper(keyRep{alias: map[string]string{oldKey: f.member}})
	require.ErrorIs(t, f.delegate(f.policy, migratedVal, 10), types.ErrValidatorOperatedByMember)

	// Another group, of which the operator is no member, may delegate to it.
	other := cosmosAddr("second_policy_______")
	f.addGroup(t, "Second", other, cosmosAddr("second_member_______"), "")
	require.NoError(t, f.delegate(other, memberVal, 10))

	// Redelegation checks the destination the same way.
	require.NoError(t, f.delegate(f.policy, f.val, 10))
	_, err := f.ms.RedelegateTreasury(f.ctx, &types.MsgRedelegateTreasury{
		Authority: f.policy, SrcValidatorAddress: f.val, DstValidatorAddress: memberVal, Amount: uspark(10),
	})
	require.ErrorIs(t, err, types.ErrValidatorOperatedByMember)
}

func TestDelegateTreasury_ValidatorLimit(t *testing.T) {
	f := setupTreasury(t)
	f.sk.outside = math.NewInt(1_000_000)

	vals := make([]string, keeper.MaxTreasuryValidatorsPerGroup+1)
	for i := range vals {
		vals[i] = f.sk.addValidator(cosmosAddr("limit_operator_____" + string(rune('a'+i))))
	}
	for _, v := range vals[:keeper.MaxTreasuryValidatorsPerGroup] {
		require.NoError(t, f.delegate(f.policy, v, 5))
	}
	last := vals[keeper.MaxTreasuryValidatorsPerGroup]
	require.ErrorIs(t, f.delegate(f.policy, last, 5), types.ErrTooManyTreasuryValidators)
	f.sk.revert(f.policy, last, 5)
	// Adding to a validator it already stakes with is fine.
	require.NoError(t, f.delegate(f.policy, vals[0], 5))
}

// A group past its term may not stake more, but may always undelegate, and
// may redelegate (which puts nothing more at stake).
func TestTreasuryStaking_TermGates(t *testing.T) {
	f := setupTreasury(t)
	second := f.sk.addValidator(cosmosAddr("second_operator_____"))
	require.NoError(t, f.delegate(f.policy, f.val, 100))

	expired := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(48 * time.Hour))
	_, err := f.ms.DelegateTreasury(expired, &types.MsgDelegateTreasury{
		Authority: f.policy, ValidatorAddress: f.val, Amount: uspark(10),
	})
	require.ErrorIs(t, err, types.ErrGroupExpired)
	_, err = f.ms.RedelegateTreasury(expired, &types.MsgRedelegateTreasury{
		Authority: f.policy, SrcValidatorAddress: f.val, DstValidatorAddress: second, Amount: uspark(10),
	})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(10), f.sk.delegated(f.policy, second))

	res, err := f.ms.UndelegateTreasury(expired, &types.MsgUndelegateTreasury{
		Authority: f.policy, ValidatorAddress: f.val, Amount: uspark(40),
	})
	require.NoError(t, err)
	require.Equal(t, expired.BlockTime().Add(fakeUnbondingTime).Unix(), res.CompletionTime)
	require.Equal(t, math.NewInt(50), f.sk.delegated(f.policy, f.val))

	// In term, redelegation moves the stake.
	rres, err := f.ms.RedelegateTreasury(f.ctx, &types.MsgRedelegateTreasury{
		Authority: f.policy, SrcValidatorAddress: f.val, DstValidatorAddress: second, Amount: uspark(50),
	})
	require.NoError(t, err)
	require.Positive(t, rres.CompletionTime)
	require.Equal(t, math.NewInt(60), f.sk.delegated(f.policy, second))
}

func TestWithdrawTreasuryRewards(t *testing.T) {
	f := setupTreasury(t)
	stranger := cosmosAddr("any_passing_account_")
	require.NoError(t, f.delegate(f.policy, f.val, 100))
	f.sk.rewards[f.policy] = math.NewInt(7)
	before := f.bank.balance[bankKey(f.policy)]

	// Anyone may trigger it; the rewards land in the treasury.
	res, err := f.ms.WithdrawTreasuryRewards(f.ctx, &types.MsgWithdrawTreasuryRewards{
		Creator: stranger, PolicyAddress: f.policy, ValidatorAddress: f.val,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(uspark(7)), res.Amount)
	require.Equal(t, before.Add(uspark(7)), f.bank.balance[bankKey(f.policy)])
	require.True(t, f.bank.balance[bankKey(stranger)].IsZero())

	// It cannot be pointed at an account that is no treasury.
	f.sk.rewards[stranger] = math.NewInt(7)
	_, err = f.ms.WithdrawTreasuryRewards(f.ctx, &types.MsgWithdrawTreasuryRewards{
		Creator: stranger, PolicyAddress: stranger, ValidatorAddress: f.val,
	})
	require.ErrorIs(t, err, types.ErrNotTreasury)
}

// Deleting a group with stake never fails; the EndBlocker unbonds the stake
// and sweeps it to the parent once it matures.
func TestDeleteGroup_WindsDownTreasuryStake(t *testing.T) {
	f := setupTreasury(t)
	parent := cosmosAddr("parent_policy_______")
	f.addGroup(t, "Parent", parent, cosmosAddr("parent_member_______"), "")
	child := cosmosAddr("child_policy________")
	f.addGroup(t, "Child", child, cosmosAddr("child_member________"), parent)
	second := f.sk.addValidator(cosmosAddr("second_operator_____"))

	require.NoError(t, f.delegate(child, f.val, 100))
	require.NoError(t, f.delegate(child, second, 50))
	parentBefore := f.bank.balance[bankKey(parent)]
	childLiquid := f.bank.balance[bankKey(child)]

	_, err := f.ms.DeleteGroup(f.ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Child"})
	require.NoError(t, err)
	// The liquid treasury went to the parent at once.
	require.Equal(t, parentBefore.Add(childLiquid...), f.bank.balance[bankKey(parent)])

	// The stake is still treasury stake: it counts against the cap and is
	// left out of the gov tally until it is unbonded.
	addrs, err := f.k.TreasuryAddresses(f.ctx)
	require.NoError(t, err)
	require.Contains(t, addrs, child)
	staked, _, _, err := f.k.TreasuryStake(f.ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(150), staked)

	// First EndBlock: one validator refuses for now, the other unbonds.
	f.sk.refuse[second] = true
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(f.ctx))
	require.True(t, f.sk.delegated(child, f.val).IsZero())
	require.Equal(t, math.NewInt(50), f.sk.delegated(child, second))
	require.Equal(t, 1, countWindDowns(t, f))

	// Rewards of a winding-down treasury can still be collected.
	f.sk.rewards[child] = math.NewInt(3)
	_, err = f.ms.WithdrawTreasuryRewards(f.ctx, &types.MsgWithdrawTreasuryRewards{
		Creator: f.member, PolicyAddress: child, ValidatorAddress: second,
	})
	require.NoError(t, err)

	// The retry undelegates the rest.
	f.sk.refuse[second] = false
	later := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(2 * time.Hour))
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(later))
	require.True(t, f.sk.delegated(child, second).IsZero())
	require.Equal(t, 1, countWindDowns(t, f))

	// Nothing happens before the entries mature.
	almost := later.WithBlockTime(later.BlockTime().Add(fakeUnbondingTime - time.Hour))
	f.sk.mature(almost.BlockTime())
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(almost))
	require.Equal(t, 1, countWindDowns(t, f))

	// Matured: the unbonded SPARK and the rewards reach the parent and the
	// entry is gone.
	done := later.WithBlockTime(later.BlockTime().Add(fakeUnbondingTime + time.Hour))
	f.sk.mature(done.BlockTime())
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(done))
	require.Zero(t, countWindDowns(t, f))
	require.True(t, f.bank.balance[bankKey(child)].IsZero())
	require.Equal(t, parentBefore.Add(childLiquid...).Add(uspark(153)), f.bank.balance[bankKey(parent)])
	addrs, err = f.k.TreasuryAddresses(done)
	require.NoError(t, err)
	require.NotContains(t, addrs, child)
}

// A deleted group without stake queues nothing.
func TestDeleteGroup_NoStakeNoWindDown(t *testing.T) {
	f := setupTreasury(t)
	parent := cosmosAddr("parent_policy_______")
	f.addGroup(t, "Parent", parent, cosmosAddr("parent_member_______"), "")
	child := cosmosAddr("child_policy________")
	f.addGroup(t, "Child", child, cosmosAddr("child_member________"), parent)

	_, err := f.ms.DeleteGroup(f.ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Child"})
	require.NoError(t, err)
	require.Zero(t, countWindDowns(t, f))
}

func countWindDowns(t *testing.T, f *treasuryFixture) int {
	t.Helper()
	n := 0
	require.NoError(t, f.k.TreasuryWindDowns.Walk(f.ctx, nil, func(collections.Pair[int64, string], string) (bool, error) {
		n++
		return false, nil
	}))
	return n
}

func TestTreasuryWindDowns_GenesisRoundTrip(t *testing.T) {
	f := setupTreasury(t)
	policy := cosmosAddr("deleted_policy______")
	heir := cosmosAddr("heir_policy_________")
	require.NoError(t, f.k.TreasuryWindDowns.Set(f.ctx, collections.Join(int64(42), policy), heir))

	gen, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, []types.TreasuryWindDown{{PolicyAddress: policy, Heir: heir, Due: 42}}, gen.TreasuryWindDowns)

	// Validation reads addresses with the global bech32 prefix.
	entry := types.TreasuryWindDown{PolicyAddress: sdk.AccAddress("deleted_policy______").String(), Due: 42}
	dup := types.GenesisState{Params: types.DefaultParams(), TreasuryWindDowns: []types.TreasuryWindDown{entry}}
	require.NoError(t, dup.Validate())
	dup.TreasuryWindDowns = append(dup.TreasuryWindDowns, entry)
	require.ErrorContains(t, dup.Validate(), "duplicate treasury wind-down")
	dup.TreasuryWindDowns = []types.TreasuryWindDown{{PolicyAddress: "not-an-address"}}
	require.ErrorContains(t, dup.Validate(), "invalid treasury wind-down policy address")
}

func TestMigrateV3ToV4GrantsTreasuryStaking(t *testing.T) {
	f := setupTreasury(t)
	council := cosmosAddr("commons_council_pol_")
	f.addGroup(t, "Commons Council", council, cosmosAddr("council_member______"), "")

	require.NoError(t, f.k.MigrateV3ToV4(f.ctx))
	require.NoError(t, f.k.MigrateV3ToV4(f.ctx)) // idempotent

	perms, err := f.k.PolicyPermissions.Get(f.ctx, council)
	require.NoError(t, err)
	for _, m := range keeper.TreasuryStakingMsgs {
		n := 0
		for _, have := range perms.AllowedMessages {
			if have == m {
				n++
			}
		}
		require.Equal(t, 1, n, m)
	}
	// A group outside the genesis list is left alone.
	perms, err = f.k.PolicyPermissions.Get(f.ctx, f.policy)
	require.NoError(t, err)
	require.NotContains(t, perms.AllowedMessages, keeper.TreasuryStakingMsgs[0])
}

// A policy can only stake through the wrappers: the raw staking messages
// can never be put on an allowlist.
func TestRawStakingMessagesForbidden(t *testing.T) {
	for _, m := range []string{
		"/cosmos.staking.v1beta1.MsgDelegate",
		"/cosmos.staking.v1beta1.MsgBeginRedelegate",
		"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation",
		"/cosmos.staking.v1beta1.MsgCreateValidator",
	} {
		require.True(t, types.ForbiddenMessages[m], m)
	}
	for _, m := range keeper.TreasuryStakingMsgs {
		require.False(t, types.ForbiddenMessages[m], m)
	}
}

// A group still in its pre-launch phase may not stake.
func TestDelegateTreasury_ActivationGate(t *testing.T) {
	f := setupTreasury(t)
	g, err := f.k.Groups.Get(f.ctx, "Stakers")
	require.NoError(t, err)
	g.ActivationTime = f.ctx.BlockTime().Unix() + 3600
	require.NoError(t, f.k.Groups.Set(f.ctx, "Stakers", g))

	require.ErrorIs(t, f.delegate(f.policy, f.val, 10), types.ErrGroupNotActive)
	require.Empty(t, f.sk.routed)
}

// Without x/staking wired every staking message is refused, the query
// reports the error, and deleting a group still works.
func TestTreasuryStaking_StakingKeeperUnwired(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)
	policy := cosmosAddr("stakers_policy______")
	parent := cosmosAddr("parent_policy_______")
	setupProposalState(t, k, ctx, "Parent", parent, cosmosAddr("parent_member_______"))
	setupProposalState(t, k, ctx, "Stakers", policy, cosmosAddr("stakers_member______"))
	g, err := k.Groups.Get(ctx, "Stakers")
	require.NoError(t, err)
	g.ParentPolicyAddress = parent
	require.NoError(t, k.Groups.Set(ctx, "Stakers", g))

	_, err = ms.DelegateTreasury(ctx, &types.MsgDelegateTreasury{
		Authority: policy, ValidatorAddress: valoperOf(cosmosAddr("outside_operator____")), Amount: uspark(10),
	})
	require.ErrorContains(t, err, "staking keeper not wired")
	_, err = keeper.NewQueryServerImpl(k).TreasuryStake(ctx, &types.QueryTreasuryStakeRequest{})
	require.Error(t, err)
	_, err = keeper.NewQueryServerImpl(k).TreasuryStake(ctx, nil)
	require.Error(t, err)

	_, err = ms.DeleteGroup(ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Stakers"})
	require.NoError(t, err)
	require.NoError(t, k.EndBlockTreasuryWindDowns(ctx))
}

// Undelegation and redelegation are refused for an address that is no
// treasury, and a redelegation may not exceed the validator limit.
func TestUndelegateRedelegateTreasury_Refusals(t *testing.T) {
	f := setupTreasury(t)
	f.sk.outside = math.NewInt(1_000_000)
	stranger := cosmosAddr("some_plain_account__")

	_, err := f.ms.UndelegateTreasury(f.ctx, &types.MsgUndelegateTreasury{Authority: stranger, ValidatorAddress: f.val, Amount: uspark(1)})
	require.ErrorIs(t, err, types.ErrNotTreasury)
	_, err = f.ms.RedelegateTreasury(f.ctx, &types.MsgRedelegateTreasury{
		Authority: stranger, SrcValidatorAddress: f.val, DstValidatorAddress: f.val, Amount: uspark(1),
	})
	require.ErrorIs(t, err, types.ErrNotTreasury)
	_, err = f.ms.UndelegateTreasury(f.ctx, &types.MsgUndelegateTreasury{Authority: f.policy, ValidatorAddress: f.val, Amount: uspark(0)})
	require.ErrorIs(t, err, types.ErrInvalidTreasuryStake)
	// Nothing is staked yet: x/staking refuses, and the wrapper reports it.
	_, err = f.ms.UndelegateTreasury(f.ctx, &types.MsgUndelegateTreasury{Authority: f.policy, ValidatorAddress: f.val, Amount: uspark(5)})
	require.ErrorContains(t, err, "undelegation failed")

	vals := make([]string, keeper.MaxTreasuryValidatorsPerGroup)
	for i := range vals {
		vals[i] = f.sk.addValidator(cosmosAddr("limit_operator_____" + string(rune('a'+i))))
		require.NoError(t, f.delegate(f.policy, vals[i], 5))
	}
	_, err = f.ms.RedelegateTreasury(f.ctx, &types.MsgRedelegateTreasury{
		Authority: f.policy, SrcValidatorAddress: vals[0], DstValidatorAddress: f.val, Amount: uspark(1),
	})
	require.ErrorIs(t, err, types.ErrTooManyTreasuryValidators)
	f.sk.move(f.policy, f.val, math.NewInt(-1))
	f.sk.move(f.policy, vals[0], math.NewInt(1))

	// At the limit, a whole delegation can still move to a new validator:
	// the count is taken after the move.
	_, err = f.ms.RedelegateTreasury(f.ctx, &types.MsgRedelegateTreasury{
		Authority: f.policy, SrcValidatorAddress: vals[0], DstValidatorAddress: f.val, Amount: uspark(5),
	})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(5), f.sk.delegated(f.policy, f.val))
}

// A deleted group whose parent is not a group (x/gov) has no heir: its stake
// is still unbonded, and the SPARK stays at the deleted policy address.
func TestTreasuryWindDown_NoHeirKeepsFunds(t *testing.T) {
	f := setupTreasury(t)
	gov := f.k.GetModuleAddressByName("gov").String()
	g, err := f.k.Groups.Get(f.ctx, "Stakers")
	require.NoError(t, err)
	g.ParentPolicyAddress = gov
	require.NoError(t, f.k.Groups.Set(f.ctx, "Stakers", g))
	require.NoError(t, f.delegate(f.policy, f.val, 100))
	liquid := f.bank.balance[bankKey(f.policy)]

	_, err = f.ms.DeleteGroup(f.ctx, &types.MsgDeleteGroup{Authority: gov, GroupName: "Stakers"})
	require.NoError(t, err)
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(f.ctx))
	require.True(t, f.sk.delegated(f.policy, f.val).IsZero())

	done := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(fakeUnbondingTime + time.Hour))
	f.sk.mature(done.BlockTime())
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(done))
	require.Zero(t, countWindDowns(t, f))
	require.Equal(t, liquid.Add(uspark(100)), f.bank.balance[bankKey(f.policy)])
}

// One EndBlock handles at most MaxTreasuryWindDownsPerBlock entries; the
// rest wait for the next block. An entry not yet due is left alone.
func TestEndBlockTreasuryWindDowns_PerBlockCap(t *testing.T) {
	f := setupTreasury(t)
	now := f.ctx.BlockTime().Unix()
	total := keeper.MaxTreasuryWindDownsPerBlock + 2
	for i := 0; i < total; i++ {
		// Nothing staked: each entry finishes the first time it is handled.
		policy := cosmosAddr("wound_down_policy__" + string(rune('a'+i)))
		require.NoError(t, f.k.TreasuryWindDowns.Set(f.ctx, collections.Join(now, policy), ""))
	}
	future := cosmosAddr("future_policy_______")
	require.NoError(t, f.k.TreasuryWindDowns.Set(f.ctx, collections.Join(now+3600, future), ""))

	require.NoError(t, f.k.EndBlockTreasuryWindDowns(f.ctx))
	require.Equal(t, total-keeper.MaxTreasuryWindDownsPerBlock+1, countWindDowns(t, f))
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(f.ctx))
	require.Equal(t, 1, countWindDowns(t, f))
	winding, err := f.k.TreasuryAddresses(f.ctx)
	require.NoError(t, err)
	require.Contains(t, winding, future)
}

// InitGenesis restores the wind-down queue under its due time.
func TestTreasuryWindDowns_GenesisImport(t *testing.T) {
	f := setupTreasury(t)
	policy := cosmosAddr("deleted_policy______")
	heir := cosmosAddr("heir_policy_________")
	gen, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	gen.TreasuryWindDowns = []types.TreasuryWindDown{{PolicyAddress: policy, Heir: heir, Due: 42}}

	k2, ctx2, _ := setupCommonsKeeper(t)
	require.NoError(t, k2.InitGenesis(ctx2, *gen))
	got, err := k2.TreasuryWindDowns.Get(ctx2, collections.Join(int64(42), policy))
	require.NoError(t, err)
	require.Equal(t, heir, got)
}

// A delegation counts against max_spend_per_epoch like a spend.
func TestDelegateTreasury_EpochSpendLimit(t *testing.T) {
	f := setupTreasury(t)
	limit := math.NewInt(100)
	g, err := f.k.Groups.Get(f.ctx, "Stakers")
	require.NoError(t, err)
	g.MaxSpendPerEpoch = &limit
	require.NoError(t, f.k.Groups.Set(f.ctx, "Stakers", g))

	require.ErrorIs(t, f.delegate(f.policy, f.val, 101), types.ErrRateLimitExceeded)
	require.Empty(t, f.sk.routed)
	require.NoError(t, f.delegate(f.policy, f.val, 60))
	// Cumulative over the epoch, and shared with ordinary spends.
	require.ErrorIs(t, f.delegate(f.policy, f.val, 41), types.ErrRateLimitExceeded)
	require.ErrorIs(t, f.k.CheckSpendPreconditions(f.ctx, f.policy, sdk.NewCoins(uspark(41))), types.ErrRateLimitExceeded)
	require.NoError(t, f.delegate(f.policy, f.val, 40))

	// Moving or unbonding stake uses no budget.
	second := f.sk.addValidator(cosmosAddr("second_operator_____"))
	_, err = f.ms.RedelegateTreasury(f.ctx, &types.MsgRedelegateTreasury{
		Authority: f.policy, SrcValidatorAddress: f.val, DstValidatorAddress: second, Amount: uspark(100),
	})
	require.NoError(t, err)

	// The next day the budget is back.
	tomorrow := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(20 * time.Hour))
	if tomorrow.BlockTime().Unix()/86400 == f.ctx.BlockTime().Unix()/86400 {
		tomorrow = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(23 * time.Hour))
	}
	_, err = f.ms.DelegateTreasury(tomorrow, &types.MsgDelegateTreasury{Authority: f.policy, ValidatorAddress: second, Amount: uspark(100)})
	require.NoError(t, err)
}

// A treasury may only delegate or redelegate to a bonded validator: the cap
// compares treasury stake with bonded stake.
func TestDelegateTreasury_UnbondedValidatorRefused(t *testing.T) {
	f := setupTreasury(t)
	jailed := f.sk.addValidator(cosmosAddr("jailed_operator_____"))
	v := f.sk.vals[jailed]
	v.Status = stakingtypes.Unbonding
	v.Jailed = true
	f.sk.vals[jailed] = v

	require.ErrorIs(t, f.delegate(f.policy, jailed, 10), types.ErrInvalidTreasuryStake)
	require.Empty(t, f.sk.routed)

	require.NoError(t, f.delegate(f.policy, f.val, 10))
	_, err := f.ms.RedelegateTreasury(f.ctx, &types.MsgRedelegateTreasury{
		Authority: f.policy, SrcValidatorAddress: f.val, DstValidatorAddress: jailed, Amount: uspark(10),
	})
	require.ErrorIs(t, err, types.ErrInvalidTreasuryStake)
}

// The member-validator rule also covers the members of the groups above the
// delegating group.
func TestDelegateTreasury_AncestorMemberValidatorRefused(t *testing.T) {
	f := setupTreasury(t)
	council := cosmosAddr("parent_policy_______")
	councilMember := cosmosAddr("parent_member_______")
	f.addGroup(t, "Council", council, councilMember, "")
	committee := cosmosAddr("child_policy________")
	f.addGroup(t, "Committee", committee, cosmosAddr("child_member________"), council)
	councilVal := f.sk.addValidator(councilMember)

	require.ErrorIs(t, f.delegate(committee, councilVal, 10), types.ErrValidatorOperatedByMember)
	require.ErrorIs(t, f.delegate(council, councilVal, 10), types.ErrValidatorOperatedByMember)
	// An unrelated group may.
	require.NoError(t, f.delegate(f.policy, councilVal, 10))
}

// Stake that comes to sit with a member's validator because the membership
// changed is unbonded when the change is made, for the group and the groups
// below it.
func TestMemberChange_UnbondsStakeWithMemberValidator(t *testing.T) {
	f := setupTreasury(t)
	council := cosmosAddr("parent_policy_______")
	f.addGroup(t, "Council", council, cosmosAddr("parent_member_______"), "")
	committee := cosmosAddr("child_policy________")
	f.addGroup(t, "Committee", committee, cosmosAddr("child_member________"), council)

	operator := cosmosAddr("future_member_______")
	opVal := f.sk.addValidator(operator)
	require.NoError(t, f.delegate(committee, opVal, 40))
	require.NoError(t, f.delegate(committee, f.val, 30))
	require.NoError(t, f.delegate(f.policy, opVal, 20))

	// The operator joins the council.
	require.NoError(t, f.k.AddMember(f.ctx, "Council", types.Member{Address: operator, Weight: "1"}))
	require.NoError(t, f.k.UnbondMemberValidatorStake(f.ctx, "Council"))

	require.True(t, f.sk.delegated(committee, opVal).IsZero())
	require.Equal(t, math.NewInt(30), f.sk.delegated(committee, f.val))
	// An unrelated group's stake is untouched.
	require.Equal(t, math.NewInt(20), f.sk.delegated(f.policy, opVal))
}

// When x/staking refuses that unbonding, the membership change still goes
// through, and the treasury may not stake more until it has moved the
// stake, which a redelegation can do.
func TestMemberChange_RefusedUnbondBlocksFurtherStaking(t *testing.T) {
	f := setupTreasury(t)
	operator := cosmosAddr("future_member_______")
	opVal := f.sk.addValidator(operator)
	second := f.sk.addValidator(cosmosAddr("second_operator_____"))
	require.NoError(t, f.delegate(f.policy, opVal, 40))

	require.NoError(t, f.k.AddMember(f.ctx, "Stakers", types.Member{Address: operator, Weight: "1"}))
	f.sk.refuse[opVal] = true
	require.NoError(t, f.k.UnbondMemberValidatorStake(f.ctx, "Stakers"))
	require.Equal(t, math.NewInt(40), f.sk.delegated(f.policy, opVal))

	require.ErrorIs(t, f.delegate(f.policy, f.val, 10), types.ErrValidatorOperatedByMember)
	f.sk.revert(f.policy, f.val, 10)

	_, err := f.ms.RedelegateTreasury(f.ctx, &types.MsgRedelegateTreasury{
		Authority: f.policy, SrcValidatorAddress: opVal, DstValidatorAddress: second, Amount: uspark(40),
	})
	require.NoError(t, err)
	require.NoError(t, f.delegate(f.policy, f.val, 10))
}

// A wind-down whose heir is deleted before the stake matures passes to the
// heir's own parent instead of stranding the SPARK at a dead address.
func TestTreasuryWindDown_HeirDeletedPassesToItsParent(t *testing.T) {
	f := setupTreasury(t)
	top := cosmosAddr("grand_policy________")
	f.addGroup(t, "Top", top, cosmosAddr("grand_member________"), "")
	parent := cosmosAddr("parent_policy_______")
	f.addGroup(t, "Parent", parent, cosmosAddr("parent_member_______"), top)
	child := cosmosAddr("child_policy________")
	f.addGroup(t, "Child", child, cosmosAddr("child_member________"), parent)

	require.NoError(t, f.delegate(child, f.val, 100))
	_, err := f.ms.DeleteGroup(f.ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Child"})
	require.NoError(t, err)
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(f.ctx))

	_, err = f.ms.DeleteGroup(f.ctx, &types.MsgDeleteGroup{Authority: top, GroupName: "Parent"})
	require.NoError(t, err)
	topBefore := f.bank.balance[bankKey(top)]

	done := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(fakeUnbondingTime + time.Hour))
	f.sk.mature(done.BlockTime())
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(done))
	require.Zero(t, countWindDowns(t, f))
	require.True(t, f.bank.balance[bankKey(child)].IsZero())
	require.Equal(t, topBefore.Add(uspark(100)), f.bank.balance[bankKey(top)])
}

// A wind-down never finishes with a delegation still in place: dust below
// one base unit is unbonded by its shares, and a delegation that cannot be
// valued keeps the entry (and the address in TreasuryAddresses).
func TestTreasuryWindDown_LeavesNoDelegationBehind(t *testing.T) {
	f := setupTreasury(t)
	parent := cosmosAddr("parent_policy_______")
	f.addGroup(t, "Parent", parent, cosmosAddr("parent_member_______"), "")
	child := cosmosAddr("child_policy________")
	f.addGroup(t, "Child", child, cosmosAddr("child_member________"), parent)
	slashed := f.sk.addValidator(cosmosAddr("slashed_operator____"))
	gone := f.sk.addValidator(cosmosAddr("vanished_operator___"))

	require.NoError(t, f.delegate(child, slashed, 5))
	require.NoError(t, f.delegate(child, gone, 5))
	_, err := f.ms.DeleteGroup(f.ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Child"})
	require.NoError(t, err)

	// One validator is slashed to nothing (its shares are now dust), the
	// other cannot be read.
	v := f.sk.vals[slashed]
	v.Tokens = math.ZeroInt()
	f.sk.vals[slashed] = v
	goneVal := f.sk.vals[gone]
	delete(f.sk.vals, gone)

	require.NoError(t, f.k.EndBlockTreasuryWindDowns(f.ctx))
	require.Equal(t, []string{slashed}, f.sk.unbondedShares)
	require.True(t, f.sk.delegated(child, slashed).IsZero())
	require.Equal(t, 1, countWindDowns(t, f), "an unreadable delegation must keep the entry")
	addrs, err := f.k.TreasuryAddresses(f.ctx)
	require.NoError(t, err)
	require.Contains(t, addrs, child)

	// Readable again: the retry unbonds it and the wind-down can finish.
	f.sk.vals[gone] = goneVal
	later := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(2 * time.Hour))
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(later))
	require.True(t, f.sk.delegated(child, gone).IsZero())
	done := later.WithBlockTime(later.BlockTime().Add(fakeUnbondingTime + time.Hour))
	f.sk.mature(done.BlockTime())
	require.NoError(t, f.k.EndBlockTreasuryWindDowns(done))
	require.Zero(t, countWindDowns(t, f))
}

// TreasuryStakers follows the wrapper: written by the first delegation,
// removed when the group is deleted, and carried through genesis.
func TestTreasuryStakersIndex(t *testing.T) {
	f := setupTreasury(t)
	parent := cosmosAddr("parent_policy_______")
	f.addGroup(t, "Parent", parent, cosmosAddr("parent_member_______"), "")
	child := cosmosAddr("child_policy________")
	f.addGroup(t, "Child", child, cosmosAddr("child_member________"), parent)

	addrs, err := f.k.TreasuryAddresses(f.ctx)
	require.NoError(t, err)
	require.Empty(t, addrs, "a group that never staked is not walked")

	require.NoError(t, f.delegate(child, f.val, 10))
	require.NoError(t, f.delegate(child, f.val, 10))
	addrs, err = f.k.TreasuryAddresses(f.ctx)
	require.NoError(t, err)
	require.Equal(t, []string{child}, addrs)
	msg, broken := keeper.TreasuryStakersInvariant(f.k)(f.ctx)
	require.False(t, broken, msg)

	gen, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, []string{child}, gen.TreasuryStakers)
	bad := *gen
	bad.TreasuryStakers = []string{"not-an-address"}
	require.ErrorContains(t, bad.Validate(), "invalid treasury staker address")

	// A staked treasury missing from the index breaks the invariant.
	require.NoError(t, f.k.TreasuryStakers.Remove(f.ctx, child))
	_, broken = keeper.TreasuryStakersInvariant(f.k)(f.ctx)
	require.True(t, broken)
	require.NoError(t, f.k.TreasuryStakers.Set(f.ctx, child))

	// Deleted: the wind-down queue carries the address from here on.
	_, err = f.ms.DeleteGroup(f.ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Child"})
	require.NoError(t, err)
	has, err := f.k.TreasuryStakers.Has(f.ctx, child)
	require.NoError(t, err)
	require.False(t, has)
	addrs, err = f.k.TreasuryAddresses(f.ctx)
	require.NoError(t, err)
	require.Equal(t, []string{child}, addrs)
}

// A raw staking message granted before it became forbidden is stripped by
// the migration, and is refused at submission and at execution even while
// it is still on a stored allowlist.
func TestForbiddenMessageOnStoredAllowlist(t *testing.T) {
	f := setupTreasury(t)
	const raw = "/cosmos.staking.v1beta1.MsgDelegate"
	perms, err := f.k.PolicyPermissions.Get(f.ctx, f.policy)
	require.NoError(t, err)
	perms.AllowedMessages = append(perms.AllowedMessages, raw)
	require.NoError(t, f.k.PolicyPermissions.Set(f.ctx, f.policy, perms))

	anyMsg, err := codectypes.NewAnyWithValue(&stakingtypes.MsgDelegate{
		DelegatorAddress: f.policy, ValidatorAddress: f.val, Amount: uspark(10),
	})
	require.NoError(t, err)
	_, err = f.ms.SubmitProposal(f.ctx, &types.MsgSubmitProposal{
		Proposer: f.member, PolicyAddress: f.policy, Messages: []*codectypes.Any{anyMsg},
	})
	require.ErrorContains(t, err, "globally forbidden")
	err = f.k.CheckProposalPermissions(f.ctx, types.Proposal{PolicyAddress: f.policy, Messages: []*codectypes.Any{anyMsg}})
	require.ErrorContains(t, err, "globally forbidden")

	// The migration strips it, keeps the rest, and indexes existing stakers.
	require.NoError(t, f.delegate(f.policy, f.val, 10))
	require.NoError(t, f.k.TreasuryStakers.Remove(f.ctx, f.policy))
	require.NoError(t, f.k.MigrateV3ToV4(f.ctx))
	perms, err = f.k.PolicyPermissions.Get(f.ctx, f.policy)
	require.NoError(t, err)
	require.NotContains(t, perms.AllowedMessages, raw)
	require.Contains(t, perms.AllowedMessages, "/sparkdream.commons.v1.MsgUpdateParams")
	has, err := f.k.TreasuryStakers.Has(f.ctx, f.policy)
	require.NoError(t, err)
	require.True(t, has)
}
