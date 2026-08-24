package keeper_test

import (
	"testing"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// A bounty is how the people who want a particular initiative looked at bid
// reviewer attention up, once the completion gate makes that attention scarce.
// The rules that keep it from becoming something worse are all here: it pays
// per verdict rather than on approval, it cannot be yanked once reviewers have
// committed, and it refunds rather than forfeits when nobody shows up.

func TestBountyPaysPerVerdictNotOnApproval(t *testing.T) {
	// The rule that keeps a bounty from being a bribe: it splits across the
	// verdicts FILED on the resolving round, whatever those verdicts said. A
	// rejecting reviewer is paid exactly like an approving one.
	//
	// The verdict is written directly rather than through SubmitInitiativeReview
	// because a rejection bounces the round, moving the initiative to a fresh
	// one — a real flow (rounds exhausting into abandonment pays the rejecting
	// reviewers), but a roundabout way to assert the split itself.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	funder := sdk.AccAddress([]byte("bounty-funder---"))
	mkReviewMember(t, k, ctx, funder, "100.0")
	total, err := k.EscrowReviewBounty(ctx, funder, rf.initiative, math.NewInt(1_000))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_000), total)

	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.NoError(t, k.InitiativeReview.Set(ctx,
		collections.Join3(initiative.Id, initiative.ReviewRound, rf.reviewer.String()),
		types.InitiativeReview{
			InitiativeId: initiative.Id,
			Round:        initiative.ReviewRound,
			Reviewer:     rf.reviewer.String(),
			Approved:     false, // rejected, and paid all the same
			BondReserved: math.ZeroInt(),
		}))

	before, err := k.GetMember(ctx, rf.reviewer)
	require.NoError(t, err)

	paid, err := k.PayReviewBounty(ctx, initiative)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_000), paid, "a rejecting verdict earns the bounty too")

	after, err := k.GetMember(ctx, rf.reviewer)
	require.NoError(t, err)
	require.Equal(t, before.DreamBalance.Add(math.NewInt(1_000)).String(), after.DreamBalance.String())

	funderAfter, err := k.GetMember(ctx, funder)
	require.NoError(t, err)
	require.True(t, funderAfter.StakedDream.IsZero(), "the escrow is drawn down, not left locked")
}

func TestBountyCannotBeReclaimedOnceAVerdictIsFiled(t *testing.T) {
	// The bait-and-switch guard: reviewers commit bond and do the reading on
	// the strength of the advertised bounty. Letting the funder withdraw after
	// that would make funding a way to waste a reviewer's collateral.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	funder := sdk.AccAddress([]byte("bounty-funder2--"))
	mkReviewMember(t, k, ctx, funder, "100.0")
	_, err := k.EscrowReviewBounty(ctx, funder, rf.initiative, math.NewInt(1_000))
	require.NoError(t, err)

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		true, nil, "looks done"))

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	matured := ctx.WithBlockHeight(ctx.BlockHeight() + int64(params.ReviewBountyReclaimDelay) + 1)

	_, err = k.WithdrawReviewBounty(matured, funder, rf.initiative)
	require.Error(t, err, "committed bounties cannot be withdrawn even after the delay")
	require.Contains(t, err.Error(), "committed")
}

func TestBountyReclaimRequiresTheDelay(t *testing.T) {
	// Without a delay, advertising a bounty and pulling it in the same block is
	// free — a way to grief the reviewer roster at no cost.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	funder := sdk.AccAddress([]byte("bounty-funder3--"))
	mkReviewMember(t, k, ctx, funder, "100.0")
	_, err := k.EscrowReviewBounty(ctx, funder, rf.initiative, math.NewInt(1_000))
	require.NoError(t, err)

	_, err = k.WithdrawReviewBounty(ctx, funder, rf.initiative)
	require.Error(t, err, "reclaim before the delay must be refused")

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	matured := ctx.WithBlockHeight(ctx.BlockHeight() + int64(params.ReviewBountyReclaimDelay) + 1)

	refunded, err := k.WithdrawReviewBounty(matured, funder, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_000), refunded)

	member, err := k.GetMember(matured, funder)
	require.NoError(t, err)
	require.True(t, member.StakedDream.IsZero(), "reclaimed bounty is unlocked, not left staked")
}

func TestBountyOnlyRefundsItsOwnFunder(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	a := sdk.AccAddress([]byte("bounty-funder-a-"))
	b := sdk.AccAddress([]byte("bounty-funder-b-"))
	mkReviewMember(t, k, ctx, a, "100.0")
	mkReviewMember(t, k, ctx, b, "100.0")
	_, err := k.EscrowReviewBounty(ctx, a, rf.initiative, math.NewInt(600))
	require.NoError(t, err)
	_, err = k.EscrowReviewBounty(ctx, b, rf.initiative, math.NewInt(400))
	require.NoError(t, err)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	matured := ctx.WithBlockHeight(ctx.BlockHeight() + int64(params.ReviewBountyReclaimDelay) + 1)

	refunded, err := k.WithdrawReviewBounty(matured, a, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(600), refunded, "a funder reclaims their own stake, not a share of the pot")

	remaining := k.GetReviewBounty(matured, rf.initiative)
	require.Equal(t, math.NewInt(400), remaining.Amount, "the other funder's contribution stands")
}

func TestBountyRefundsWhenNobodyReviews(t *testing.T) {
	// Funding must not be a gamble on someone else's behaviour: an initiative
	// that ends with no verdict returns every contribution.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	funder := sdk.AccAddress([]byte("bounty-funder4--"))
	mkReviewMember(t, k, ctx, funder, "100.0")
	before, err := k.GetMember(ctx, funder)
	require.NoError(t, err)

	_, err = k.EscrowReviewBounty(ctx, funder, rf.initiative, math.NewInt(1_000))
	require.NoError(t, err)

	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	paid, err := k.PayReviewBounty(ctx, initiative)
	require.NoError(t, err)
	require.True(t, paid.IsZero())

	after, err := k.GetMember(ctx, funder)
	require.NoError(t, err)
	require.Equal(t, before.DreamBalance.String(), after.DreamBalance.String(),
		"an unclaimed bounty comes back whole")
	require.True(t, after.StakedDream.IsZero())

	_, gErr := k.ReviewBounty.Get(ctx, rf.initiative)
	require.Error(t, gErr, "the record is cleared rather than lingering empty")
}

func TestPermissionlessCreationEscrowsTheMinimumBounty(t *testing.T) {
	// Permissionless work mints against a self-declared budget and its review
	// fee is minted too, so without this the reviewers are funded purely by
	// dilution — the outcome the funded path's budget-netting exists to avoid.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("perm-bounty-cr--"))
	mkReviewMember(t, k, ctx, creator, "100.0")

	projectID, err := k.CreateProject(ctx, creator, "Perm", "D", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	// Budget must clear review_required_above_budget, or no review is mandatory
	// and no bounty is owed — see TestUngatedPermissionlessWorkPaysNoMinimumBounty.
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	budget := params.ReviewRequiredAboveBudget.MulRaw(2)

	initID, err := k.CreateInitiative(ctx, creator, projectID, "T", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
	require.NoError(t, err)

	bounty := k.GetReviewBounty(ctx, initID)
	require.Equal(t, params.PermissionlessMinReviewBountyRate.MulInt(budget).TruncateInt(), bounty.Amount,
		"10% of budget, in existing DREAM")
	require.Len(t, bounty.Contributions, 1)
	require.Equal(t, creator.String(), bounty.Contributions[0].Funder)
}

func TestPermissionlessCreationFailsWithoutBountyFunds(t *testing.T) {
	// The brake must bite at creation rather than being discovered later.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("perm-broke-cr---"))
	mkReviewMember(t, k, ctx, creator, "100.0")

	projectID, err := k.CreateProject(ctx, creator, "Perm", "D", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	budget := params.ReviewRequiredAboveBudget.MulRaw(2)
	wantBounty := params.PermissionlessMinReviewBountyRate.MulInt(budget).TruncateInt()

	// Enough to cover the creation fee but not the bounty, so the failure is
	// unambiguously the bounty rather than the fee.
	member, err := k.GetMember(ctx, creator)
	require.NoError(t, err)
	require.True(t, wantBounty.GT(params.InitiativeCreationFeeStandard),
		"the fixture only isolates the bounty while it costs more than the fee")
	member.DreamBalance = keeperPtrInt(params.InitiativeCreationFeeStandard.AddRaw(1))
	require.NoError(t, k.Member.Set(ctx, creator.String(), member))

	_, err = k.CreateInitiative(ctx, creator, projectID, "T", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
	require.Error(t, err, "a creator who cannot fund the review must not be able to commission the mint")
	require.Contains(t, err.Error(), "review bounty",
		"and the error must name the bounty, not leave them guessing at the creation fee")
}

func keeperPtrInt(i math.Int) *math.Int { return &i }

// The minimum bounty pays for MANDATORY review, so it must not be charged where
// review is optional. review_required_above_budget equals the APPRENTICE
// ceiling, which makes apprentice work permanently ungated — and apprentice
// work is the on-ramp: reachable at PROVISIONAL, by members who join holding
// exactly zero DREAM (Member.DreamBalance starts at zero on invitation accept).
// Charging there took 10x the creation fee for a review that could never
// happen.
func TestUngatedPermissionlessWorkPaysNoMinimumBounty(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("apprentice-cr---"))
	mkReviewMember(t, k, ctx, creator, "100.0")

	projectID, err := k.CreateProject(ctx, creator, "Perm", "D", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, params.ApprenticeTier.MaxBudget.String(), params.ReviewRequiredAboveBudget.String(),
		"this test only means anything while the gate sits on the apprentice ceiling")

	// At the ceiling exactly: the gate compares with a strict >, so this is
	// ungated and must carry no bounty.
	initID, err := k.CreateInitiative(ctx, creator, projectID, "Apprentice work", "d", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, params.ApprenticeTier.MaxBudget)
	require.NoError(t, err)

	require.True(t, k.GetReviewBounty(ctx, initID).Amount.IsZero(),
		"ungated work must not be charged for review that will never be required")
}

func TestGatedPermissionlessWorkStillPaysTheMinimum(t *testing.T) {
	// The other half: where review IS mandatory, permissionless work still pays
	// for it, or the reviewers are funded purely by dilution.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("standard-cr-----"))
	mkReviewMember(t, k, ctx, creator, "100.0")

	projectID, err := k.CreateProject(ctx, creator, "Perm", "D", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	budget := params.ReviewRequiredAboveBudget.AddRaw(1_000_000) // just over the gate

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Standard work", "d", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
	require.NoError(t, err)

	want := params.PermissionlessMinReviewBountyRate.MulInt(budget).TruncateInt()
	require.True(t, want.IsPositive())
	require.Equal(t, want, k.GetReviewBounty(ctx, initID).Amount,
		"gated permissionless work funds the review it makes mandatory")
}
