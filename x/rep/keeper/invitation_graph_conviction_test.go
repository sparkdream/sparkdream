package keeper_test

import (
	"testing"
	"time"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// External conviction is the only brake left on a permissionless, self-assigned
// mint that does not depend on somebody choosing to challenge. It used to be a
// pure identity test: exclude the four known insiders, count everyone else.
//
// Membership comes from an invitation, and an invitation is a vouching
// relationship the inviter bonded for. So the cheapest way past the floor was
// never to defeat it — it was to manufacture the electorate: invite a few
// accounts, gift them DREAM (GiftOnlyToInvitees permits exactly the
// inviter -> own invitee direction), and have them vouch. These tests pin the
// one-hop exclusion that closes it, and pin the hop count so a later change
// cannot quietly widen it into an unbounded subtree walk.

// mkInvitedMember creates an ESTABLISHED member recorded as invited by `inviter`.
// Pass an empty inviter for a member with no invitation edge.
func mkInvitedMember(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, inviter string) {
	t.Helper()
	require.NoError(t, k.Member.Set(ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     PtrInt(math.NewInt(10_000_000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		InvitedBy:        inviter,
	}))
}

func TestInviteeOfAnAffiliateIsNotExternal(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	assignee := sdk.AccAddress([]byte("ig-assignee-addr"))
	puppet := sdk.AccAddress([]byte("ig-puppet-addr--"))
	mkInvitedMember(t, k, ctx, assignee, "")
	mkInvitedMember(t, k, ctx, puppet, assignee.String())

	n := k.InvitationNeighborhoodOf(ctx, assignee.String())

	require.False(t, k.IsStakerExternalTo(ctx, assignee.String(), n),
		"the affiliate itself is never external")
	require.False(t, k.IsStakerExternalTo(ctx, puppet.String(), n),
		"an account the affiliate invited is the affiliate's own conviction wearing a second address")
}

func TestInviterOfAnAffiliateIsNotExternal(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// The other direction: the staker vouched for the insider first, with a
	// bond, before this initiative existed.
	inviter := sdk.AccAddress([]byte("ig-inviter-addr-"))
	assignee := sdk.AccAddress([]byte("ig-invited-asgn-"))
	mkInvitedMember(t, k, ctx, inviter, "")
	mkInvitedMember(t, k, ctx, assignee, inviter.String())

	n := k.InvitationNeighborhoodOf(ctx, assignee.String())
	require.False(t, k.IsStakerExternalTo(ctx, inviter.String(), n),
		"the affiliate's own inviter is not an arm's-length backer")
}

func TestInvitationExclusionStopsAtOneHop(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// root invited both the assignee and the sibling, so the sibling is two
	// hops from the assignee. Excluding siblings would mean walking the
	// subtree, which is unbounded per-block work any member can inflate for
	// the price of more invitations.
	root := sdk.AccAddress([]byte("ig-root-address-"))
	assignee := sdk.AccAddress([]byte("ig-sib-assignee-"))
	sibling := sdk.AccAddress([]byte("ig-sibling-addr-"))
	grandchild := sdk.AccAddress([]byte("ig-grandchild---"))
	mkInvitedMember(t, k, ctx, root, "")
	mkInvitedMember(t, k, ctx, assignee, root.String())
	mkInvitedMember(t, k, ctx, sibling, root.String())
	mkInvitedMember(t, k, ctx, grandchild, sibling.String())

	n := k.InvitationNeighborhoodOf(ctx, assignee.String())
	require.True(t, k.IsStakerExternalTo(ctx, sibling.String(), n),
		"siblings share an inviter but are two hops apart and stay external")
	require.True(t, k.IsStakerExternalTo(ctx, grandchild.String(), n),
		"three hops away is external")
}

func TestNonMemberStakerIsStillTestedByIdentity(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// A missing member record must not crash the sweep or silently mark an
	// affiliate external; it simply contributes no invitation edge.
	assignee := sdk.AccAddress([]byte("ig-nm-assignee--"))
	stranger := sdk.AccAddress([]byte("ig-nm-stranger--"))

	n := k.InvitationNeighborhoodOf(ctx, assignee.String())
	require.False(t, k.IsStakerExternalTo(ctx, assignee.String(), n))
	require.True(t, k.IsStakerExternalTo(ctx, stranger.String(), n))
	require.False(t, k.IsStakerExternalTo(ctx, "", n), "an empty address is never external")
}

func TestExternalConvictionExcludesTheAssigneesInvitee(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// End to end through the conviction sweep: the number that gates completion
	// must not count a puppet the assignee invited.
	assignee := sdk.AccAddress([]byte("ig-e2e-assignee-"))
	puppet := sdk.AccAddress([]byte("ig-e2e-puppet---"))
	outsider := sdk.AccAddress([]byte("ig-e2e-outsider-"))
	mkInvitedMember(t, k, ctx, assignee, "")
	mkInvitedMember(t, k, ctx, puppet, assignee.String())
	mkInvitedMember(t, k, ctx, outsider, "")

	projectID, err := k.CreateProject(ctx, assignee, "Perm", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	initID, err := k.CreateInitiative(ctx, assignee, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)

	_, err = k.CreateStake(ctx, puppet, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(5000))
	require.NoError(t, err)

	// Let the stakes mature so conviction is non-trivial.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(14 * 24 * time.Hour))
	require.NoError(t, k.UpdateInitiativeConviction(ctx, initID))

	withPuppetOnly, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.True(t, keeper.DerefDec(withPuppetOnly.CurrentConviction).IsPositive(),
		"the puppet's stake still counts toward total conviction")
	require.True(t, keeper.DerefDec(withPuppetOnly.ExternalConviction).IsZero(),
		"an account the assignee invited contributes no external conviction, got %s",
		keeper.DerefDec(withPuppetOnly.ExternalConviction).String())

	// A genuinely unrelated staker does count.
	_, err = k.CreateStake(ctx, outsider, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(5000))
	require.NoError(t, err)
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(14 * 24 * time.Hour))
	require.NoError(t, k.UpdateInitiativeConviction(ctx, initID))

	withOutsider, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.True(t, keeper.DerefDec(withOutsider.ExternalConviction).IsPositive(),
		"an unrelated staker is external")
}

func TestCompletionBonusWithholdsFromAuthorAndProjectOwner(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// The completion bonus was the third and last place in the module that
	// defined affiliation differently from the other two: it excluded the
	// assignee and apprentice only, so the initiative's author and the parent
	// project's creator were paid as though they were independent backers.
	// Both are in InitiativeAffiliates for every other purpose.
	owner := sdk.AccAddress([]byte("cb-owner-address"))
	author := sdk.AccAddress([]byte("cb-author-addres"))
	assignee := sdk.AccAddress([]byte("cb-assignee-addr"))
	invitee := sdk.AccAddress([]byte("cb-invitee-addre"))
	outsider := sdk.AccAddress([]byte("cb-outsider-addr"))
	mkInvitedMember(t, k, ctx, owner, "")
	mkInvitedMember(t, k, ctx, author, "")
	mkInvitedMember(t, k, ctx, assignee, "")
	mkInvitedMember(t, k, ctx, invitee, assignee.String())
	mkInvitedMember(t, k, ctx, outsider, "")

	projectID, err := k.CreateProject(ctx, owner, "Proj", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1_000_000), math.NewInt(100_000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, owner,
		math.NewInt(1_000_000), math.NewInt(100_000)))

	initID, err := k.CreateInitiative(ctx, author, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(10_000))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))

	for _, s := range []sdk.AccAddress{owner, author, assignee, invitee, outsider} {
		_, err = k.CreateStake(ctx, s, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2_000))
		require.NoError(t, err)
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(14 * 24 * time.Hour))

	pre := make(map[string]math.Int)
	for _, s := range []sdk.AccAddress{owner, author, assignee, invitee, outsider} {
		m, err := k.Member.Get(ctx, s.String())
		require.NoError(t, err)
		pre[s.String()] = *m.DreamBalance
	}

	require.NoError(t, k.DistributeInitiativeCompletionBonus(ctx, initID, math.NewInt(10_000)))

	received := func(a sdk.AccAddress) math.Int {
		m, err := k.Member.Get(ctx, a.String())
		require.NoError(t, err)
		return m.DreamBalance.Sub(pre[a.String()])
	}

	require.True(t, received(owner).IsZero(), "the project's creator is an affiliate")
	require.True(t, received(author).IsZero(), "the initiative's author is an affiliate")
	require.True(t, received(assignee).IsZero(), "the assignee already takes the completer share")
	require.True(t, received(invitee).IsZero(), "an account the assignee invited is not an independent backer")
	require.True(t, received(outsider).IsPositive(),
		"an unrelated staker is exactly who the bonus exists to reward")
}
