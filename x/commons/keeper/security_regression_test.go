package keeper_test

import (
	"errors"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"sparkdream/testutil"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"
)

// Regression tests for the 2026-10-26 security review, Part 8 (x/commons).

// cosmosCodec matches setupCommonsKeeper's address codec; sdk.AccAddress.
// String() follows the global bech32 config, which another test in this
// package switches to the chain prefix.
var cosmosCodec = addresscodec.NewBech32Codec("cosmos")

func bech(a sdk.AccAddress) string {
	s, err := cosmosCodec.BytesToString(a)
	if err != nil {
		panic(err)
	}
	return s
}

// useCosmosPrefix pins the global bech32 account prefix to "cosmos" for the
// test (msg_update_params_test.go switches it to the chain prefix, and the
// keeper parses member addresses with the global config).
func useCosmosPrefix(t *testing.T) {
	t.Helper()
	cfg := sdk.GetConfig()
	prevAcc, prevPub := cfg.GetBech32AccountAddrPrefix(), cfg.GetBech32AccountPubPrefix()
	cfg.SetBech32PrefixForAccount("cosmos", "cosmospub")
	t.Cleanup(func() { cfg.SetBech32PrefixForAccount(prevAcc, prevPub) })
}

func addr(s string) string {
	b := make([]byte, 20)
	copy(b, s)
	return bech(b)
}

// seedCommonsCouncil registers a "Commons Council" policy with an allowlist
// and a spend cap, the only non-gov signer MsgRegisterGroup accepts.
func seedCommonsCouncil(t *testing.T, k keeper.Keeper, ctx sdk.Context, allowed []string) string {
	t.Helper()
	policy := bech(keeper.DeriveCouncilAddress(900, "standard"))
	cap := math.NewInt(1_000_000)
	require.NoError(t, k.Groups.Set(ctx, "Commons Council", types.Group{
		GroupId: 900, PolicyAddress: policy, ParentPolicyAddress: addr("gov"),
		MaxSpendPerEpoch: &cap, MinMembers: 1, MaxMembers: 10,
		CurrentTermExpiration: ctx.BlockTime().Unix() + 1_000_000,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, policy, "Commons Council"))
	require.NoError(t, k.PolicyPermissions.Set(ctx, policy, types.PolicyPermissions{PolicyAddress: policy, AllowedMessages: allowed}))
	return policy
}

func childMsg(authority string) *types.MsgRegisterGroup {
	cap := math.NewInt(1000)
	return &types.MsgRegisterGroup{
		Authority:        authority,
		Name:             "Aurora Guild",
		Members:          []string{addr("aurora1"), addr("aurora2")},
		MemberWeights:    []string{"1", "1"},
		MinMembers:       2,
		MaxMembers:       5,
		TermDuration:     86400,
		VoteThreshold:    testutil.DecPtr("0.5"),
		PolicyType:       keeper.PolicyTypePercentage,
		VotingPeriod:     3600,
		MaxSpendPerEpoch: &cap,
		AllowedMessages:  []string{"/sparkdream.commons.v1.MsgSpendFromCommons"},
	}
}

// CMN-01: a non-gov MsgRegisterGroup is bounded by the Commons Council's
// own powers.
func TestRegisterGroup_NonGovIsBounded(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.NewParams("")))
	ms := keeper.NewMsgServerImpl(k)
	council := seedCommonsCouncil(t, k, ctx, []string{
		"/sparkdream.commons.v1.MsgSpendFromCommons",
		"/sparkdream.commons.v1.MsgRegisterGroup",
	})

	// A registered group that is not the Commons Council is refused.
	other := bech(keeper.DeriveCouncilAddress(901, "standard"))
	require.NoError(t, k.Groups.Set(ctx, "Zenith Board", types.Group{PolicyAddress: other}))
	require.NoError(t, k.PolicyToName.Set(ctx, other, "Zenith Board"))
	_, err := ms.RegisterGroup(ctx, childMsg(other))
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)

	cases := []struct {
		desc string
		mut  func(m *types.MsgRegisterGroup)
	}{
		{"forbidden delegation msg", func(m *types.MsgRegisterGroup) {
			m.AllowedMessages = []string{"/sparkdream.session.v1.MsgCreateGrant"}
		}},
		{"msg the parent does not hold", func(m *types.MsgRegisterGroup) {
			m.AllowedMessages = []string{"/sparkdream.commons.v1.MsgUpdateGroupConfig"}
		}},
		{"funding weight", func(m *types.MsgRegisterGroup) { m.FundingWeight = 10000 }},
		{"no spend cap", func(m *types.MsgRegisterGroup) { m.MaxSpendPerEpoch = nil }},
		{"zero spend cap (= unlimited)", func(m *types.MsgRegisterGroup) { z := math.ZeroInt(); m.MaxSpendPerEpoch = &z }},
		{"spend cap above the parent's", func(m *types.MsgRegisterGroup) { c := math.NewInt(2_000_000); m.MaxSpendPerEpoch = &c }},
		{"one-member group", func(m *types.MsgRegisterGroup) {
			m.MinMembers = 1
			m.Members = m.Members[:1]
			m.MemberWeights = m.MemberWeights[:1]
		}},
		{"zero threshold", func(m *types.MsgRegisterGroup) { m.VoteThreshold = testutil.DecPtr("0") }},
		{"negative threshold", func(m *types.MsgRegisterGroup) { m.VoteThreshold = testutil.DecPtr("-1") }},
		{"threshold above 1", func(m *types.MsgRegisterGroup) { m.VoteThreshold = testutil.DecPtr("1.5") }},
		{"sub-majority threshold", func(m *types.MsgRegisterGroup) { m.VoteThreshold = testutil.DecPtr("0.2") }},
		{"nil threshold (used to panic)", func(m *types.MsgRegisterGroup) { m.VoteThreshold = nil }},
		{"reserved canonical name", func(m *types.MsgRegisterGroup) { m.Name = "Operations Committee" }},
		{"bare-key electoral authority", func(m *types.MsgRegisterGroup) { m.ElectoralPolicyAddress = addr("phoenix") }},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			m := childMsg(council)
			tc.mut(m)
			require.NotPanics(t, func() { _, err = ms.RegisterGroup(ctx, m) })
			require.Error(t, err)
		})
	}

	// The bounded request itself succeeds.
	_, err = ms.RegisterGroup(ctx, childMsg(council))
	require.NoError(t, err)
}

func TestForbiddenMessages_CoverDelegationEscapes(t *testing.T) {
	useCosmosPrefix(t)
	for _, m := range []string{
		"/sparkdream.session.v1.MsgCreateGrant",
		"/sparkdream.session.v1.MsgExecSession",
		"/sparkdream.session.v1.MsgCreateSession",
		"/sparkdream.shield.v1.MsgShieldedExec",
		"/sparkdream.guardian.v1.MsgExec",
		"/sparkdream.vault.v1.MsgCreateVault",
		"/sparkdream.vault.v1.MsgExecute",
	} {
		require.True(t, types.ForbiddenMessages[m], m)
	}
}

// CMN-02: deleting a group revokes its session grants, tombstones its
// policy so the claim hook fails closed, and sweeps its treasury.
func TestDeleteGroup_RevokesGrantsAndSweeps(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, bank := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)
	sk := newMockSessionKeeper()
	k.SetSessionKeeper(sk)

	parent := bech(keeper.DeriveCouncilAddress(1, "standard"))
	child := bech(keeper.DeriveCouncilAddress(2, "standard"))
	require.NoError(t, k.Groups.Set(ctx, "Parent Council", types.Group{GroupId: 1, PolicyAddress: parent}))
	require.NoError(t, k.PolicyToName.Set(ctx, parent, "Parent Council"))
	require.NoError(t, k.Groups.Set(ctx, "Rogue Committee", types.Group{GroupId: 2, PolicyAddress: child, ParentPolicyAddress: parent}))
	require.NoError(t, k.PolicyToName.Set(ctx, child, "Rogue Committee"))

	grantID, err := sk.CreateGrantOnBehalfOf(ctx, "commons", &sessiontypes.MsgCreateGrant{Granter: child, Grantee: addr("ally")})
	require.NoError(t, err)
	bank.balance[child] = sdk.NewCoins(sdk.NewInt64Coin("uspark", 500))

	_, err = ms.DeleteGroup(ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Rogue Committee"})
	require.NoError(t, err)

	_, err = sk.GetGrant(ctx, grantID)
	require.ErrorIs(t, err, sessiontypes.ErrGrantNotFound, "the deleted policy's grant is revoked")
	require.True(t, bank.balance[child].IsZero())
	require.Equal(t, int64(500), bank.balance[parent].AmountOf("uspark").Int64(), "treasury swept to the parent")

	tomb, err := k.DeletedPolicies.Has(ctx, child)
	require.NoError(t, err)
	require.True(t, tomb)
	hook := keeper.NewSessionClaimHook(k)
	err = hook.PreCheck(ctx, sessiontypes.Grant{Granter: child}, sdk.NewCoins(sdk.NewInt64Coin("uspark", 1)))
	require.ErrorIs(t, err, types.ErrGroupNotFound, "a former group policy's claims fail closed")
}

// CMN-03: committee size bounds are x/gov only and compiled for the
// standing committees.
func TestUpdateGroupConfig_SizeBounds(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
	parent := bech(keeper.DeriveCouncilAddress(1, "standard"))
	require.NoError(t, k.Groups.Set(ctx, "Commons Operations Committee", types.Group{
		GroupId: 3, PolicyAddress: bech(keeper.DeriveCouncilAddress(3, "standard")),
		ParentPolicyAddress: parent, MinMembers: 2, MaxMembers: 5,
	}))

	_, err := ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: parent, GroupName: "Commons Operations Committee", MinMembers: 1})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: govAddr, GroupName: "Commons Operations Committee", MaxMembers: 100})
	require.ErrorIs(t, err, types.ErrInvalidGroupSize, "compiled [2,5] holds even for x/gov")
	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: govAddr, GroupName: "Commons Operations Committee", MaxMembers: 4})
	require.NoError(t, err)
}

// setupTwoMemberCommittee registers a 2-member committee at threshold 0.5
// that may change its own membership.
func setupTwoMemberCommittee(t *testing.T, k keeper.Keeper, ctx sdk.Context) (string, string, string) {
	t.Helper()
	policy := bech(keeper.DeriveCouncilAddress(7, "standard"))
	alice, bob := addr("phoenix"), addr("aurora")
	require.NoError(t, k.Groups.Set(ctx, "Phoenix Committee", types.Group{
		GroupId: 7, PolicyAddress: policy, ParentPolicyAddress: addr("parent"),
		ElectoralPolicyAddress: policy, MinMembers: 2, MaxMembers: 5,
		CurrentTermExpiration: ctx.BlockTime().Unix() + 1_000_000,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, policy, "Phoenix Committee"))
	require.NoError(t, k.AddMember(ctx, "Phoenix Committee", types.Member{Address: alice, Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, "Phoenix Committee", types.Member{Address: bob, Weight: "1"}))
	require.NoError(t, k.DecisionPolicies.Set(ctx, policy, types.DecisionPolicy{PolicyType: "percentage", Threshold: "0.5", VotingPeriod: 3600}))
	require.NoError(t, k.PolicyPermissions.Set(ctx, policy, types.PolicyPermissions{PolicyAddress: policy, AllowedMessages: []string{
		"/sparkdream.commons.v1.MsgUpdateGroupMembers",
		"/sparkdream.commons.v1.MsgSpendFromCommons",
	}}))
	return policy, alice, bob
}

func submitAndVote(t *testing.T, k keeper.Keeper, ctx sdk.Context, policy, voter string, m sdk.Msg) types.Proposal {
	t.Helper()
	ms := keeper.NewMsgServerImpl(k)
	anyMsg, err := codectypes.NewAnyWithValue(m)
	require.NoError(t, err)
	res, err := ms.SubmitProposal(ctx, &types.MsgSubmitProposal{Proposer: voter, PolicyAddress: policy, Messages: []*codectypes.Any{anyMsg}})
	require.NoError(t, err)
	_, err = ms.VoteProposal(ctx, &types.MsgVoteProposal{Voter: voter, ProposalId: res.ProposalId, Option: types.VoteOption_VOTE_OPTION_YES})
	require.NoError(t, err)
	p, err := k.Proposals.Get(ctx, res.ProposalId)
	require.NoError(t, err)
	return p
}

// CMN-06: one member of a two-member 0.5 committee can no longer change its
// membership alone; ordinary proposals keep passing on one vote.
func TestThreshold_MembershipChangeNeedsStrictMajority(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	policy, alice, bob := setupTwoMemberCommittee(t, k, ctx)

	coup := submitAndVote(t, k, ctx, policy, alice, &types.MsgUpdateGroupMembers{
		Authority: policy, GroupPolicyAddress: policy,
		MembersToAdd: []string{addr("sock")}, WeightsToAdd: []string{"1"}, MembersToRemove: []string{bob},
	})
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, coup.Status, "1 of 2 cannot replace the peer")

	spend := submitAndVote(t, k, ctx, policy, alice, &types.MsgSpendFromCommons{Authority: policy, Recipient: addr("zenith"), Amount: sdk.NewCoins(sdk.NewInt64Coin("uspark", 1))})
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, spend.Status)

	// With the peer's vote the membership change passes.
	_, err := keeper.NewMsgServerImpl(k).VoteProposal(ctx, &types.MsgVoteProposal{Voter: bob, ProposalId: coup.Id, Option: types.VoteOption_VOTE_OPTION_YES})
	require.NoError(t, err)
	p, err := k.Proposals.Get(ctx, coup.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, p.Status)
}

// CMN-04: an accepted proposal expires after its execution window; a
// renewal invalidates proposals accepted by the old members.
func TestProposal_ExecutionWindowAndRenewal(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)
	policy, alice, _ := setupTwoMemberCommittee(t, k, ctx)

	p := submitAndVote(t, k, ctx, policy, alice, &types.MsgSpendFromCommons{Authority: policy, Recipient: addr("zenith"), Amount: sdk.NewCoins(sdk.NewInt64Coin("uspark", 1))})
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, p.Status)

	late := ctx.WithBlockTime(time.Unix(p.ExecutionTime+keeper.ProposalExecutionWindow+1, 0))
	_, err := ms.ExecuteProposal(late, &types.MsgExecuteProposal{Executor: alice, ProposalId: p.Id})
	require.Error(t, err)
	require.NoError(t, k.EndBlockProposals(late))
	p, err = k.Proposals.Get(late, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXPIRED, p.Status)

	// Renewal bumps the policy version.
	govAddr := k.GetAuthorityString()
	v0, err := k.GetPolicyVersion(ctx, policy)
	require.NoError(t, err)
	_, err = ms.RenewGroup(ctx, &types.MsgRenewGroup{Authority: govAddr, GroupName: "Phoenix Committee",
		NewMembers: []string{addr("nova1"), addr("nova2")}, NewMemberWeights: []string{"1", "1"}})
	require.NoError(t, err)
	v1, err := k.GetPolicyVersion(ctx, policy)
	require.NoError(t, err)
	require.Equal(t, v0+1, v1)
}

// CMN-07: anonymous proposals are rate-limited per council per day.
func TestAnonymousProposal_RateLimited(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)
	policy, _, _ := setupTwoMemberCommittee(t, k, ctx)
	shield := bech(authtypes.NewModuleAddress("shield"))
	for i := uint64(0); i < keeper.MaxAnonymousProposalsPerCouncilPerDay; i++ {
		_, err := ms.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{Proposer: shield, PolicyAddress: policy, Metadata: "signal"})
		require.NoError(t, err)
	}
	_, err := ms.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{Proposer: shield, PolicyAddress: policy, Metadata: "signal"})
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}

type failingFutarchy struct{ calls *int }

func (f failingFutarchy) CreateMarketInternal(sdk.Context, sdk.AccAddress, string, string, int64, int64, sdk.Coin) (uint64, error) {
	*f.calls++
	return 0, errors.New("subsidy unfunded")
}

// CMN-08: due confidence markets fire from EndBlock; a failed trigger is
// retried later instead of ending the loop.
func TestMarketTriggers_FireAndRetry(t *testing.T) {
	useCosmosPrefix(t)
	calls := 0
	k, ctx, _, _ := setupCommonsKeeperWithFutarchy(t, failingFutarchy{calls: &calls})
	require.NoError(t, k.Groups.Set(ctx, "Nova Council", types.Group{FutarchyEnabled: true, TermDuration: 1000}))
	now := ctx.BlockTime().Unix()
	require.NoError(t, k.MarketTriggerQueue.Set(ctx, collections.Join(now, "Nova Council")))

	require.NoError(t, k.EndBlockMarketTriggers(ctx))
	require.Equal(t, 1, calls)
	has, err := k.MarketTriggerQueue.Has(ctx, collections.Join(now+keeper.GovernanceMarketRetryDelay, "Nova Council"))
	require.NoError(t, err)
	require.True(t, has, "failed trigger is rescheduled")

	// The success path links the market and schedules the next one.
	k2, ctx2, _ := setupCommonsKeeper(t)
	require.NoError(t, k2.Groups.Set(ctx2, "Nova Council", types.Group{FutarchyEnabled: true, TermDuration: 1000}))
	now2 := ctx2.BlockTime().Unix()
	require.NoError(t, k2.MarketTriggerQueue.Set(ctx2, collections.Join(now2, "Nova Council")))
	require.NoError(t, k2.EndBlockMarketTriggers(ctx2))
	name, err := k2.MarketToGroup.Get(ctx2, 0)
	require.NoError(t, err)
	require.Equal(t, "Nova Council", name)
	has, err = k2.MarketTriggerQueue.Has(ctx2, collections.Join(now2+500, "Nova Council"))
	require.NoError(t, err)
	require.True(t, has)
}

// CMN-05: genesis import rebuilds the policy index for runtime groups and
// round-trips epoch spending and tombstones.
func TestGenesis_RoundTripRuntimeGroup(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	policy := bech(keeper.DeriveCouncilAddress(42, "standard"))
	veto := bech(keeper.DeriveCouncilAddress(42, "veto"))
	gs := types.DefaultGenesis()
	gs.GroupMap = []types.Group{{Index: "Aurora Guild", GroupId: 42, PolicyAddress: policy, VetoPolicyAddress: veto}}
	gs.EpochSpending = []types.EpochSpendingEntry{{PolicyAddress: policy, EpochDay: 5, Amount: "77"}}
	gs.DeletedPolicies = []string{addr("former")}
	require.NoError(t, k.InitGenesis(ctx, *gs))

	require.True(t, k.IsGroupPolicyAddress(ctx, policy))
	require.True(t, k.IsGroupPolicyAddress(ctx, veto))
	out, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, gs.EpochSpending, out.EpochSpending)
	require.Equal(t, gs.DeletedPolicies, out.DeletedPolicies)
}

// CMN-09: a migration whose target already shares a seat with the source
// is vetoed.
func TestMigrationHooks_VetoSharedSeat(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	_, alice, bob := setupTwoMemberCommittee(t, k, ctx)
	h := keeper.NewMigrationHooks(&k)
	err := h.BeforeMemberMigration(ctx, mustBytes(alice), mustBytes(bob), 0)
	require.Error(t, err)
}

// CMN-11: a council veto cannot shield its own subtree.
func TestEmergencyCancel_SubtreeDetection(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	council := bech(keeper.DeriveCouncilAddress(1, "standard"))
	veto := bech(keeper.DeriveCouncilAddress(1, "veto"))
	child := bech(keeper.DeriveCouncilAddress(2, "standard"))
	require.NoError(t, k.Groups.Set(ctx, "Council", types.Group{PolicyAddress: council, VetoPolicyAddress: veto}))
	require.NoError(t, k.PolicyToName.Set(ctx, council, "Council"))
	require.NoError(t, k.PolicyToName.Set(ctx, veto, "Council"))
	require.NoError(t, k.Groups.Set(ctx, "Child", types.Group{PolicyAddress: child, ParentPolicyAddress: council}))
	require.NoError(t, k.PolicyToName.Set(ctx, child, "Child"))

	require.True(t, k.IsPolicyInSubtree(ctx, council, council))
	require.True(t, k.IsPolicyInSubtree(ctx, veto, council))
	require.True(t, k.IsPolicyInSubtree(ctx, child, council))
	require.False(t, k.IsPolicyInSubtree(ctx, addr("elsewhere"), council))
}

// CMN-14: current_term_expiration == 0 means no expiry everywhere.
func TestTermExpiry_ZeroMeansNoExpiry(t *testing.T) {
	useCosmosPrefix(t)
	require.False(t, keeper.IsTermExpired(types.Group{CurrentTermExpiration: 0}, 1<<40))
	require.True(t, keeper.IsTermExpired(types.Group{CurrentTermExpiration: 10}, 11))
}

func mustBytes(s string) sdk.AccAddress {
	b, err := cosmosCodec.StringToBytes(s)
	if err != nil {
		panic(err)
	}
	return b
}

// FUT-07: an INVALID confidence market still clears its group link.
func TestAfterMarketResolved_InvalidClearsLink(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Groups.Set(ctx, "Nova Council", types.Group{TermDuration: 1000, CurrentTermExpiration: 5000}))
	require.NoError(t, k.MarketToGroup.Set(ctx, 9, "Nova Council"))
	require.NoError(t, k.AfterMarketResolved(ctx, 9, "invalid"))
	has, err := k.MarketToGroup.Has(ctx, 9)
	require.NoError(t, err)
	require.False(t, has)
	g, err := k.Groups.Get(ctx, "Nova Council")
	require.NoError(t, err)
	require.Equal(t, int64(5000), g.CurrentTermExpiration, "no tenure change")
}

// SES-09 (commons side): commons tells x/session it controls live council
// policies (create and revoke) and deleted ones (revoke only).
func TestManagesGranter(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	live := seedCommonsCouncil(t, k, ctx, nil)
	deleted := addr("deleted_policy")
	require.NoError(t, k.DeletedPolicies.Set(ctx, deleted))
	user := addr("aurora")

	require.True(t, k.ManagesGranter(ctx, live, false))
	require.True(t, k.ManagesGranter(ctx, live, true))
	require.False(t, k.ManagesGranter(ctx, deleted, false))
	require.True(t, k.ManagesGranter(ctx, deleted, true))
	require.False(t, k.ManagesGranter(ctx, user, false))
	require.False(t, k.ManagesGranter(ctx, user, true))
}

// CMN-06: the Commons Operations Committee is not its own electoral
// authority. Its parent, the Commons Council, elects it: a committee
// proposal cannot add or swap its own members.
func TestOpsCommitteeIsElectedByParent(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	founder := addr("phoenix")
	k.CreateTestAccount(ctx, mustBytes(founder))
	gs := types.DefaultGenesis()
	gs.FoundingMembers = []types.FoundingMember{{Address: founder, DisplayName: "phoenix", Founder: true}}
	require.NoError(t, k.InitGenesis(ctx, *gs))

	ops, err := k.Groups.Get(ctx, "Commons Operations Committee")
	require.NoError(t, err)
	council, err := k.Groups.Get(ctx, "Commons Council")
	require.NoError(t, err)
	require.Empty(t, ops.ElectoralPolicyAddress, "no electoral delegation")
	require.Equal(t, council.PolicyAddress, ops.ParentPolicyAddress)

	ms := keeper.NewMsgServerImpl(k)
	newcomer := addr("aurora")
	_, err = ms.UpdateGroupMembers(ctx, &types.MsgUpdateGroupMembers{
		Authority:          ops.PolicyAddress,
		GroupPolicyAddress: ops.PolicyAddress,
		MembersToAdd:       []string{newcomer},
		WeightsToAdd:       []string{"1"},
	})
	require.ErrorContains(t, err, "unauthorized", "the committee cannot elect itself")

	_, err = ms.UpdateGroupMembers(ctx, &types.MsgUpdateGroupMembers{
		Authority:          council.PolicyAddress,
		GroupPolicyAddress: ops.PolicyAddress,
		MembersToAdd:       []string{newcomer},
		WeightsToAdd:       []string{"1"},
	})
	require.NoError(t, err, "the parent council elects the committee")
	has, err := k.HasMember(ctx, "Commons Operations Committee", newcomer)
	require.NoError(t, err)
	require.True(t, has)
}
