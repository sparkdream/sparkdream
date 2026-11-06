package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"sparkdream/testutil"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// countingFutarchy counts CreateMarketInternal calls and hands out
// sequential market ids.
type countingFutarchy struct{ created *int }

func (m countingFutarchy) CreateMarketInternal(sdk.Context, sdk.AccAddress, string, string, int64, int64, sdk.Coin) (uint64, error) {
	*m.created++
	return uint64(*m.created), nil
}

// FUT-201: a trigger that fires while the group's confidence market is
// still open (futarchy was turned off and on again during it) opens no
// second market; the loop is rescheduled from the live one, and the next
// trigger after the market resolved opens a market again.
func TestFUT201_LiveMarketIsNotDoubled(t *testing.T) {
	created := 0
	k, ctx, _, _ := setupCommonsKeeperWithFutarchy(t, countingFutarchy{created: &created})
	const group, other = "AuroraCouncil", "ZenithCouncil"
	const term int64 = 3600
	for _, g := range []string{group, other} {
		require.NoError(t, k.Groups.Set(ctx, g, types.Group{Index: g, TermDuration: term, FutarchyEnabled: true}))
	}
	// A live market of the group, and one of another group.
	require.NoError(t, k.MarketToGroup.Set(ctx, 40, group))
	require.NoError(t, k.MarketToGroup.Set(ctx, 41, other))

	require.NoError(t, k.TriggerGovernanceMarket(ctx, group))
	require.Equal(t, 0, created, "no second market while one is open")
	var skipped bool
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == "confidence_market_skipped" {
			skipped = true
		}
	}
	require.True(t, skipped, "the skip is observable")
	next := collections.Join(ctx.BlockTime().Unix()+term/2, group)
	has, err := k.MarketTriggerQueue.Has(ctx, next)
	require.NoError(t, err)
	require.True(t, has, "the loop goes on from the live market")

	// Another group's live market does not block this group.
	require.NoError(t, k.MarketToGroup.Remove(ctx, 40))
	require.NoError(t, k.TriggerGovernanceMarket(ctx, group))
	require.Equal(t, 1, created, "a market opens once the previous one resolved")
	linked, err := k.MarketToGroup.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, group, linked)
}

// COM-202: a group policy that submits a proposal to another body (a
// council proposal executing as the policy) pays the proposal fee out of
// its treasury, so the fee passes the policy's spend gates and debits its
// per-epoch budget like any other disbursement.
func TestCOM202_PolicyProposerFeePassesSpendGates(t *testing.T) {
	k, ctx, bank, policy, _, fee := setupFeeGroup(t)
	ms := keeper.NewMsgServerImpl(k)
	spender := cosmosAddr("spender_policy______")
	cap := math.NewInt(150_000_000)
	group := types.Group{
		Index: "SpenderGroup", PolicyAddress: spender, MaxSpendPerEpoch: &cap,
		CurrentTermExpiration: ctx.BlockTime().Unix() + 86400,
	}
	require.NoError(t, k.Groups.Set(ctx, group.Index, group))
	require.NoError(t, k.PolicyToName.Set(ctx, spender, group.Index))
	require.NoError(t, k.AddMember(ctx, "FeeCouncil", types.Member{Address: spender, Weight: "1"}))
	day := ctx.BlockTime().Unix() / 86400

	// Within the cap: charged from the policy and debited.
	_, err := ms.SubmitProposal(ctx, signaling(spender, policy))
	require.NoError(t, err)
	require.Equal(t, []moduleSend{{from: accBytes(t, spender), module: authtypes.FeeCollectorName, amount: fee}}, bank.moduleSends)
	spent, err := k.EpochSpending.Get(ctx, collections.Join(spender, day))
	require.NoError(t, err)
	require.Equal(t, "100000000", spent, "the fee counts against the per-epoch budget")

	// The next fee would exceed the per-epoch cap: refused before any
	// transfer.
	bank.moduleSends = nil
	_, err = ms.SubmitProposal(ctx, signaling(spender, policy))
	require.ErrorIs(t, err, types.ErrInsufficientProposalFee)
	require.Empty(t, bank.moduleSends)

	// An expired term closes the gate too.
	cap = math.NewInt(1_000_000_000)
	group.CurrentTermExpiration = ctx.BlockTime().Unix() - 1
	require.NoError(t, k.Groups.Set(ctx, group.Index, group))
	_, err = ms.SubmitProposal(ctx, signaling(spender, policy))
	require.ErrorIs(t, err, types.ErrInsufficientProposalFee)
	require.Empty(t, bank.moduleSends)
}

// COM-203: voting_period and min_execution_period are capped by compiled
// bounds on registration and on both update paths.
func TestCOM203_DecisionPeriodBounds(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)
	require.NoError(t, k.Params.Set(ctx, types.NewParams("")))
	gov := k.GetModuleAddressByName(govtypes.ModuleName).String()

	register := func(name string, voting, minExec int64) error {
		cap := math.NewInt(1000)
		_, err := ms.RegisterGroup(ctx, &types.MsgRegisterGroup{
			Authority: gov, Name: name, Description: "d",
			Members: []string{sdk.AccAddress([]byte("com203_member_______")).String()}, MemberWeights: []string{"1"},
			MinMembers: 1, MaxMembers: 5, TermDuration: 86400,
			VoteThreshold: testutil.DecPtr("1"), PolicyType: "threshold",
			VotingPeriod: voting, MinExecutionPeriod: minExec,
			MaxSpendPerEpoch: &cap, UpdateCooldown: 3600, FundingWeight: 1,
		})
		return err
	}
	maxV, maxE := keeper.MaxVotingPeriodSeconds, keeper.MaxMinExecutionPeriodSeconds
	cases := []struct {
		desc          string
		voting, exec  int64
		errSubstr     string
		expectSuccess bool
	}{
		{"at the caps", maxV, maxE, "", true},
		{"voting period over the cap", maxV + 1, 0, "voting_period", false},
		{"execution period over the cap", 86400, maxE + 1, "min_execution_period", false},
		{"zero voting period", 0, 0, "voting_period must be greater than 0", false},
	}
	for i, tc := range cases {
		t.Run("register/"+tc.desc, func(t *testing.T) {
			err := register(fmt.Sprintf("Group%d", i), tc.voting, tc.exec)
			if tc.expectSuccess {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
			require.ErrorContains(t, err, tc.errSubstr)
		})
	}

	// Update paths: the in-place period update and the full policy
	// replacement.
	g, err := k.Groups.Get(ctx, "Group0")
	require.NoError(t, err)
	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: gov, GroupName: "Group0", VotingPeriod: maxV + 1})
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: gov, GroupName: "Group0", MinExecutionPeriod: maxE + 1})
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{
		Authority: gov, GroupName: "Group0", VoteThreshold: testutil.DecPtr("1"), PolicyType: "threshold", VotingPeriod: maxV + 1,
	})
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)

	// The voting-period-only path updates the stored policy in place and
	// bumps the version once.
	before, err := k.PolicyVersion.Get(ctx, g.PolicyAddress)
	require.NoError(t, err)
	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: gov, GroupName: "Group0", VotingPeriod: 10 * 86400, MinExecutionPeriod: 3600})
	require.NoError(t, err)
	dp, err := k.DecisionPolicies.Get(ctx, g.PolicyAddress)
	require.NoError(t, err)
	require.Equal(t, int64(10*86400), dp.VotingPeriod)
	require.Equal(t, int64(3600), dp.MinExecutionPeriod)
	require.Equal(t, "threshold", dp.PolicyType, "threshold and type untouched")
	after, err := k.PolicyVersion.Get(ctx, g.PolicyAddress)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// COM-204: the first anonymous proposal of a day drops every older day's
// counter for the council, however many days passed since the last one.
func TestCOM204_AnonCounterPrunedAcrossMultiDayGap(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)
	const council = "AnonCouncil"
	policy := sdk.AccAddress([]byte("anon_policy_________")).String()
	setupProposalState(t, k, ctx, council, policy, shieldModuleAddr())
	otherPolicy := sdk.AccAddress([]byte("other_policy________")).String()
	setupProposalState(t, k, ctx, "OtherCouncil", otherPolicy, shieldModuleAddr())
	// The council's term must outlive the gap.
	g, err := k.Groups.Get(ctx, council)
	require.NoError(t, err)
	g.CurrentTermExpiration = ctx.BlockTime().Unix() + 10*86400
	require.NoError(t, k.Groups.Set(ctx, council, g))

	submit := func(ctx sdk.Context, policy string) {
		_, err := ms.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
			Proposer: shieldModuleAddr(), PolicyAddress: policy,
			Messages: []*codectypes.Any{packMsg(t, &types.MsgUpdateParams{Authority: k.GetAuthorityString(), Params: types.DefaultParams()})},
		})
		require.NoError(t, err)
	}
	days := func(council string) []int64 {
		var out []int64
		require.NoError(t, k.AnonProposalCount.Walk(ctx, collections.NewPrefixedPairRange[string, int64](council),
			func(key collections.Pair[string, int64], _ uint64) (bool, error) {
				out = append(out, key.K2())
				return false, nil
			}))
		return out
	}
	day0 := ctx.BlockTime().Unix() / 86400
	// A bucket left from long ago and today's.
	require.NoError(t, k.AnonProposalCount.Set(ctx, collections.Join(council, day0-40), 7))
	submit(ctx, policy)
	submit(ctx, policy)
	require.Equal(t, []int64{day0}, days(council), "the first proposal of a day prunes the old bucket")
	submit(ctx, otherPolicy)

	// Three silent days, then one proposal: only the new day remains, and
	// the other council's bucket is untouched.
	later := ctx.WithBlockTime(ctx.BlockTime().Add(3 * 24 * time.Hour))
	submit(later, policy)
	require.Equal(t, []int64{day0 + 3}, days(council))
	count, err := k.AnonProposalCount.Get(ctx, collections.Join(council, day0+3))
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)
	require.Equal(t, []int64{day0}, days("OtherCouncil"))
}

// CMN-103 residual: an operational group (two seats or more) never holds
// fewer than min_members. The invariant reports the breach, and x/gov cannot
// raise min_members above the roster of an operational group; a group still
// seeded with one member is below its minimum by design.
func TestCMN103_OperationalGroupHoldsMinMembers(t *testing.T) {
	k, ctx, _ := setupSafeUpdateTest(t)
	ms := keeper.NewMsgServerImpl(k)
	gov := k.GetModuleAddressByName(govtypes.ModuleName).String()
	const group = "PhoenixGroup"
	template := createInitialGroupTemplate(ctx, sdk.AccAddress([]byte("parent_policy_______")).String())
	template.PolicyAddress = keeper.DeriveCouncilAddress(90, "standard").String()
	template.MinMembers, template.MaxMembers = 2, 5
	require.NoError(t, k.Groups.Set(ctx, group, template))
	require.NoError(t, k.PolicyToName.Set(ctx, template.PolicyAddress, group))
	require.NoError(t, k.PolicyVersion.Set(ctx, template.PolicyAddress, 1))
	addMember := func(i int) {
		require.NoError(t, k.AddMember(ctx, group, types.Member{Address: sdk.AccAddress([]byte(fmt.Sprintf("phoenix_member_%05d", i))).String(), Weight: "1"}))
	}
	broken := func() bool {
		_, b := keeper.MemberBoundsInvariant(k)(ctx)
		return b
	}

	addMember(1)
	require.False(t, broken(), "one seat: unstarted, below min by design")
	_, err := ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: gov, GroupName: group, MinMembers: 3})
	require.NoError(t, err, "an unstarted group may raise its floor")

	addMember(2)
	require.True(t, broken(), "two seats under min 3 is a breach")
	g, err := k.Groups.Get(ctx, group)
	require.NoError(t, err)
	g.MinMembers = 2
	require.NoError(t, k.Groups.Set(ctx, group, g))
	require.False(t, broken())

	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: gov, GroupName: group, MinMembers: 3})
	require.ErrorIs(t, err, types.ErrInvalidGroupSize, "operational group: grow the roster first")
	addMember(3)
	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: gov, GroupName: group, MinMembers: 3})
	require.NoError(t, err)
	require.False(t, broken())
}
