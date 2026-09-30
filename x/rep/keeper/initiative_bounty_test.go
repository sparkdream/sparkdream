package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// An initiative bounty lets members put DREAM they already hold toward work
// they want done, paid to the assignee on completion. It is also a route for
// DREAM between members, so these tests pin every rule that keeps it no
// cheaper than a tip.

type bountyFixture struct {
	f          *fixture
	projectID  uint64
	initiative uint64
	creator    sdk.AccAddress
	assignee   sdk.AccAddress
	funder     sdk.AccAddress
	budget     math.Int
}

func setupBounty(t *testing.T) *bountyFixture {
	t.Helper()
	return setupBountyOn(t, initFixture(t))
}

// setupBountyOn builds the project, OPEN initiative and three members on an
// existing fixture, so table tests that create their own fixture can use it.
func setupBountyOn(t *testing.T, f *fixture) *bountyFixture {
	t.Helper()
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("bty-creator------"))
	assignee := sdk.AccAddress([]byte("bty-assignee-----"))
	funder := sdk.AccAddress([]byte("bty-funder-------"))
	for _, a := range []sdk.AccAddress{creator, assignee, funder} {
		mkReviewMember(t, k, ctx, a, "500.0") // 100 DREAM each
	}

	budget := math.NewInt(200_000_000) // 200 DREAM
	projectID, err := k.CreateProject(ctx, creator, "BP", "D", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1_000_000_000), math.ZeroInt(), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, creator, math.NewInt(1_000_000_000), math.ZeroInt()))
	initID, err := k.CreateInitiative(ctx, creator, projectID, "T", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
	require.NoError(t, err)

	return &bountyFixture{f: f, projectID: projectID, initiative: initID,
		creator: creator, assignee: assignee, funder: funder, budget: budget}
}

func memberOf(t *testing.T, k keeper.Keeper, ctx sdk.Context, a sdk.AccAddress) types.Member {
	t.Helper()
	m, err := k.Member.Get(ctx, a.String())
	require.NoError(t, err)
	return m
}

func TestInitiativeBountyPaysTheAssigneeNetOfTax(t *testing.T) {
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	amount := math.NewInt(50_000_000)

	total, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, amount)
	require.NoError(t, err)
	require.Equal(t, amount, total)
	require.Equal(t, amount.String(), memberOf(t, k, ctx, bf.funder).StakedDream.String(), "escrow is a lock")

	require.NoError(t, k.AssignInitiativeToMember(ctx, bf.initiative, bf.assignee))
	initiative, err := k.GetInitiative(ctx, bf.initiative)
	require.NoError(t, err)
	before := memberOf(t, k, ctx, bf.assignee)
	funderBefore := memberOf(t, k, ctx, bf.funder)

	paid, err := k.PayInitiativeBounty(ctx, initiative)
	require.NoError(t, err)
	require.Equal(t, amount, paid)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	tax := params.TransferTaxRate.MulInt(amount).TruncateInt()
	net := amount.Sub(tax)
	after := memberOf(t, k, ctx, bf.assignee)
	require.Equal(t, before.DreamBalance.Add(net).String(), after.DreamBalance.String())
	require.Equal(t, net.String(), after.TransferReceivedThisEpoch.String(), "the payout counts as DREAM received")

	funderAfter := memberOf(t, k, ctx, bf.funder)
	require.True(t, funderAfter.StakedDream.IsZero())
	require.Equal(t, funderBefore.DreamBalance.Sub(amount).String(), funderAfter.DreamBalance.String())

	_, gErr := k.InitiativeBounty.Get(ctx, bf.initiative)
	require.Error(t, gErr, "the record is cleared")
}

func TestInitiativeBountyIsClippedToTheReceiveLimit(t *testing.T) {
	// A bounty cannot carry more to one account than the recipient limits
	// allow. The excess goes back to the funders untaxed.
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	setTransferParams(t, k, ctx, func(p *types.Params) {
		p.TransferTaxRate = math.LegacyZeroDec()
		p.MaxTipAmount = math.NewInt(10_000_000)
		p.MaxTipsSentPerEpoch = math.NewInt(10_000_000)
		p.MaxTransferReceivedPerEpoch = math.NewInt(30_000_000)
	})
	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(50_000_000))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, bf.initiative, bf.assignee))
	initiative, err := k.GetInitiative(ctx, bf.initiative)
	require.NoError(t, err)

	before := memberOf(t, k, ctx, bf.assignee)
	funderBefore := memberOf(t, k, ctx, bf.funder)
	paid, err := k.PayInitiativeBounty(ctx, initiative)
	require.NoError(t, err)
	require.Equal(t, "30000000", paid.String())

	after := memberOf(t, k, ctx, bf.assignee)
	require.Equal(t, before.DreamBalance.AddRaw(30_000_000).String(), after.DreamBalance.String())
	funderAfter := memberOf(t, k, ctx, bf.funder)
	require.True(t, funderAfter.StakedDream.IsZero(), "the unpaid part is unlocked, not stranded")
	require.Equal(t, funderBefore.DreamBalance.SubRaw(30_000_000).String(), funderAfter.DreamBalance.String())
}

func TestInitiativeBountyFundingRules(t *testing.T) {
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx

	// Nobody affiliated with the work may fund it.
	_, err := k.EscrowInitiativeBounty(ctx, bf.creator, bf.initiative, math.NewInt(10_000_000))
	require.ErrorIs(t, err, types.ErrUnauthorized)

	// Below the minimum contribution.
	_, err = k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(999_999))
	require.ErrorIs(t, err, types.ErrExceedsInitiativeBountyLimit)

	// No more than the budget in total (default ratio 1.0): set the per-funder
	// cap out of the way to reach it.
	setTransferParams(t, k, ctx, func(p *types.Params) {
		p.MaxInitiativeBountyPerFunderEpoch = math.NewInt(1_000_000_000)
	})
	mkReviewMember(t, k, ctx, bf.funder, "500.0")
	require.NoError(t, k.Member.Set(ctx, bf.funder.String(), func() types.Member {
		m := memberOf(t, k, ctx, bf.funder)
		m.DreamBalance = keeper.PtrInt(math.NewInt(1_000_000_000))
		return m
	}()))
	_, err = k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, bf.budget.AddRaw(1))
	require.ErrorIs(t, err, types.ErrExceedsInitiativeBountyLimit)
	_, err = k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, bf.budget)
	require.NoError(t, err)

	// A funder cannot then take the work.
	err = k.AssignInitiativeToMember(ctx, bf.initiative, bf.funder)
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestInitiativeBountyPerFunderEpochCap(t *testing.T) {
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	p := setTransferParams(t, k, ctx, func(p *types.Params) {
		p.MaxInitiativeBountyPerFunderEpoch = math.NewInt(30_000_000)
	})

	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(20_000_000))
	require.NoError(t, err)
	_, err = k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(20_000_000))
	require.ErrorIs(t, err, types.ErrExceedsInitiativeBountyLimit)

	next := ctx.WithBlockHeight(ctx.BlockHeight() + p.EpochBlocks)
	_, err = k.EscrowInitiativeBounty(next, bf.funder, bf.initiative, math.NewInt(20_000_000))
	require.NoError(t, err, "the allowance resets each epoch")
}

func TestInitiativeBountyOnlyOnOpenOrAssignedWork(t *testing.T) {
	// Funding after submission is paying a known person for a known result.
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	require.NoError(t, k.AssignInitiativeToMember(ctx, bf.initiative, bf.assignee))
	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.NoError(t, err, "ASSIGNED work can be funded")

	require.NoError(t, k.SubmitInitiativeWork(ctx, bf.initiative, bf.assignee, "ipfs://work"))
	_, err = k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.ErrorIs(t, err, types.ErrInvalidInitiativeStatus)
}

func TestInitiativeBountyReclaim(t *testing.T) {
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.NoError(t, err)

	_, err = k.WithdrawInitiativeBounty(ctx, bf.funder, bf.initiative)
	require.Error(t, err, "reclaim before the delay is refused")

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	matured := ctx.WithBlockHeight(ctx.BlockHeight() + int64(params.InitiativeBountyReclaimDelay) + 1)

	// Once assigned, the bounty is committed to whoever is doing the work.
	require.NoError(t, k.AssignInitiativeToMember(matured, bf.initiative, bf.assignee))
	_, err = k.WithdrawInitiativeBounty(matured, bf.funder, bf.initiative)
	require.ErrorIs(t, err, types.ErrInvalidInitiativeStatus)

	// Released back to OPEN, it can be reclaimed again.
	require.NoError(t, k.UnassignInitiative(matured, bf.initiative, "stepping back", false))
	refunded, err := k.WithdrawInitiativeBounty(matured, bf.funder, bf.initiative)
	require.NoError(t, err)
	require.Equal(t, "10000000", refunded.String())
	require.True(t, memberOf(t, k, matured, bf.funder).StakedDream.IsZero())
}

func TestInitiativeBountyRefundsInFullOnClose(t *testing.T) {
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	before := memberOf(t, k, ctx, bf.funder)
	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.NoError(t, err)

	require.NoError(t, k.CloseInitiative(ctx, bf.initiative, "no longer needed"))

	after := memberOf(t, k, ctx, bf.funder)
	require.Equal(t, before.DreamBalance.String(), after.DreamBalance.String(), "no tax on a refund")
	require.True(t, after.StakedDream.IsZero())
	_, gErr := k.InitiativeBounty.Get(ctx, bf.initiative)
	require.Error(t, gErr)
}

func TestInitiativeBountySurvivesGenesisRoundTrip(t *testing.T) {
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.NoError(t, err)

	gs, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Len(t, gs.InitiativeBountyList, 1)
	require.Equal(t, "10000000", gs.InitiativeBountyList[0].Amount.String())
}

func TestInitiativeBountyRefundsWhenAChallengeIsUpheld(t *testing.T) {
	// The work failed, so the bounty goes back whole — and the upheld-challenge
	// path must settle it, or the funder's DREAM stays locked on a terminal
	// initiative.
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	require.NoError(t, k.AssignInitiativeToMember(ctx, bf.initiative, bf.assignee))
	before := memberOf(t, k, ctx, bf.funder)
	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.NoError(t, err)
	require.NoError(t, k.SubmitInitiativeWork(ctx, bf.initiative, bf.assignee, "ipfs://work"))

	challenger := sdk.AccAddress([]byte("bty-challenger---"))
	mkReviewMember(t, k, ctx, challenger, "100.0")
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	challengeID, err := k.CreateChallenge(ctx, challenger, bf.initiative, "bad work",
		[]string{"evidence"}, params.MinChallengeStake)
	require.NoError(t, err)
	require.NoError(t, k.UpholdChallenge(ctx, challengeID))

	initiative, err := k.GetInitiative(ctx, bf.initiative)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_REJECTED, initiative.Status)
	after := memberOf(t, k, ctx, bf.funder)
	require.Equal(t, before.DreamBalance.String(), after.DreamBalance.String(), "refunded whole, untaxed")
	require.True(t, after.StakedDream.IsZero())
	_, gErr := k.InitiativeBounty.Get(ctx, bf.initiative)
	require.Error(t, gErr)
}

func TestInitiativeBountyRefundsWhenTheProjectIsCancelled(t *testing.T) {
	// The cascade closes initiatives itself rather than through
	// CloseInitiative, so it needs its own settlement hook.
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	before := memberOf(t, k, ctx, bf.funder)
	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.NoError(t, err)

	require.NoError(t, k.CancelProject(ctx, bf.projectID, "pivoting"))

	after := memberOf(t, k, ctx, bf.funder)
	require.Equal(t, before.DreamBalance.String(), after.DreamBalance.String())
	require.True(t, after.StakedDream.IsZero())
	_, gErr := k.InitiativeBounty.Get(ctx, bf.initiative)
	require.Error(t, gErr)
}

func TestInitiativeBountyClippedPayoutSplitsAcrossFunders(t *testing.T) {
	// Two funders, a payout clipped by the assignee's receive limit: each is
	// drawn in proportion to what they put in, the rest comes back, and the
	// tax on what was paid lands in their lifetime_burned in the same ratio.
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	second := sdk.AccAddress([]byte("bty-funder-two---"))
	mkReviewMember(t, k, ctx, second, "500.0")
	p := setTransferParams(t, k, ctx, func(p *types.Params) {
		p.TransferTaxRate = math.LegacyNewDecWithPrec(10, 2) // 10%, to make the split visible
		p.MaxTransferReceivedPerEpoch = math.NewInt(27_000_000)
	})

	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(40_000_000))
	require.NoError(t, err)
	_, err = k.EscrowInitiativeBounty(ctx, second, bf.initiative, math.NewInt(20_000_000))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, bf.initiative, bf.assignee))
	initiative, err := k.GetInitiative(ctx, bf.initiative)
	require.NoError(t, err)

	f1Before, f2Before := memberOf(t, k, ctx, bf.funder), memberOf(t, k, ctx, second)
	aBefore := memberOf(t, k, ctx, bf.assignee)
	paid, err := k.PayInitiativeBounty(ctx, initiative)
	require.NoError(t, err)

	// A 27 DREAM net at 10% tax needs a 30 DREAM gross.
	require.Equal(t, "30000000", paid.String())
	aAfter := memberOf(t, k, ctx, bf.assignee)
	require.Equal(t, aBefore.DreamBalance.AddRaw(27_000_000).String(), aAfter.DreamBalance.String())
	require.Equal(t, p.MaxTransferReceivedPerEpoch.String(), aAfter.TransferReceivedThisEpoch.String())

	f1After, f2After := memberOf(t, k, ctx, bf.funder), memberOf(t, k, ctx, second)
	require.Equal(t, f1Before.DreamBalance.SubRaw(20_000_000).String(), f1After.DreamBalance.String(), "2/3 of the gross")
	require.Equal(t, f2Before.DreamBalance.SubRaw(10_000_000).String(), f2After.DreamBalance.String(), "1/3 of the gross")
	require.True(t, f1After.StakedDream.IsZero() && f2After.StakedDream.IsZero(), "unpaid parts are unlocked")
	require.Equal(t, keeper.DerefInt(f1Before.LifetimeBurned).AddRaw(2_000_000).String(), f1After.LifetimeBurned.String())
	require.Equal(t, keeper.DerefInt(f2Before.LifetimeBurned).AddRaw(1_000_000).String(), f2After.LifetimeBurned.String())
}

func TestInitiativeBountyMsgAndQueryServers(t *testing.T) {
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	ms := keeper.NewMsgServerImpl(k)
	qs := keeper.NewQueryServerImpl(k)

	// A non-member cannot fund.
	outsider := sdk.AccAddress([]byte("bty-outsider-----"))
	_, err := ms.FundInitiativeBounty(ctx, &types.MsgFundInitiativeBounty{
		Funder: outsider.String(), InitiativeId: bf.initiative, Amount: math.NewInt(10_000_000)})
	require.ErrorIs(t, err, types.ErrNotMember)

	resp, err := ms.FundInitiativeBounty(ctx, &types.MsgFundInitiativeBounty{
		Funder: bf.funder.String(), InitiativeId: bf.initiative, Amount: math.NewInt(10_000_000)})
	require.NoError(t, err)
	require.Equal(t, "10000000", resp.Total.String())

	// The query agrees with the handler on reclaimability.
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	q, err := qs.InitiativeBounty(ctx, &types.QueryInitiativeBountyRequest{InitiativeId: bf.initiative})
	require.NoError(t, err)
	require.Equal(t, "10000000", q.Bounty.Amount.String())
	require.Len(t, q.ReclaimStatus, 1)
	require.False(t, q.ReclaimStatus[0].Reclaimable, "not before the delay")
	require.Equal(t, ctx.BlockHeight()+int64(params.InitiativeBountyReclaimDelay), q.ReclaimStatus[0].ReclaimableAtHeight)

	matured := ctx.WithBlockHeight(ctx.BlockHeight() + int64(params.InitiativeBountyReclaimDelay))
	q, err = qs.InitiativeBounty(matured, &types.QueryInitiativeBountyRequest{InitiativeId: bf.initiative})
	require.NoError(t, err)
	require.True(t, q.ReclaimStatus[0].Reclaimable)

	require.NoError(t, k.AssignInitiativeToMember(matured, bf.initiative, bf.assignee))
	q, err = qs.InitiativeBounty(matured, &types.QueryInitiativeBountyRequest{InitiativeId: bf.initiative})
	require.NoError(t, err)
	require.False(t, q.ReclaimStatus[0].Reclaimable, "assignment wins over the clock")
	_, err = ms.ReclaimInitiativeBounty(matured, &types.MsgReclaimInitiativeBounty{
		Funder: bf.funder.String(), InitiativeId: bf.initiative})
	require.ErrorIs(t, err, types.ErrInvalidInitiativeStatus)

	// No bounty: an empty record, not an error.
	q, err = qs.InitiativeBounty(ctx, &types.QueryInitiativeBountyRequest{InitiativeId: 999})
	require.NoError(t, err)
	require.True(t, q.Bounty.Amount.IsZero())
	_, err = qs.InitiativeBounty(ctx, nil)
	require.Error(t, err)
}

func TestZeroedFunderDoesNotStrandTheBounty(t *testing.T) {
	// Zeroing burns a member's whole balance, locked DREAM included. Their
	// bounty contribution must go with it: left in place, settling the bounty
	// tried to unlock DREAM that no longer existed, and completing, closing,
	// rejecting or cancelling the initiative failed every time.
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	other := sdk.AccAddress([]byte("bty-funder-three-"))
	mkReviewMember(t, k, ctx, other, "500.0")

	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.NoError(t, err)
	_, err = k.EscrowInitiativeBounty(ctx, other, bf.initiative, math.NewInt(5_000_000))
	require.NoError(t, err)

	require.NoError(t, k.ZeroMember(ctx, bf.funder, "misconduct"))

	b := k.GetInitiativeBounty(ctx, bf.initiative)
	require.Equal(t, "5000000", b.Amount.String(), "only the other funder's escrow remains")
	require.Len(t, b.Contributions, 1)
	require.Equal(t, other.String(), b.Contributions[0].Funder)

	// Settlement still works: closing refunds the remaining funder in full.
	otherBefore := memberOf(t, k, ctx, other)
	require.NoError(t, k.CloseInitiative(ctx, bf.initiative, "retired"))
	otherAfter := memberOf(t, k, ctx, other)
	require.True(t, otherAfter.StakedDream.IsZero())
	require.Equal(t, otherBefore.DreamBalance.String(), otherAfter.DreamBalance.String())
	_, gErr := k.InitiativeBounty.Get(ctx, bf.initiative)
	require.Error(t, gErr)
}

func TestZeroedSoleFunderClearsTheBounty(t *testing.T) {
	bf := setupBounty(t)
	k, ctx := bf.f.keeper, bf.f.ctx
	_, err := k.EscrowInitiativeBounty(ctx, bf.funder, bf.initiative, math.NewInt(10_000_000))
	require.NoError(t, err)

	require.NoError(t, k.ZeroMember(ctx, bf.funder, "misconduct"))

	_, gErr := k.InitiativeBounty.Get(ctx, bf.initiative)
	require.Error(t, gErr, "an emptied bounty is removed, not left at zero")
	require.NoError(t, k.AssignInitiativeToMember(ctx, bf.initiative, bf.assignee))
	initiative, err := k.GetInitiative(ctx, bf.initiative)
	require.NoError(t, err)
	paid, err := k.PayInitiativeBounty(ctx, initiative)
	require.NoError(t, err)
	require.True(t, paid.IsZero())
}
