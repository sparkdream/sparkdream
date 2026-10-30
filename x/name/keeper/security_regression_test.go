package keeper_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// Regression tests for the 2026-10-26 security review, Part 8 (x/name).

func nameAddr(s string) string {
	b := make([]byte, 20)
	copy(b, s)
	return sdk.AccAddress(b).String()
}

// seedDispute writes an owned name with an active uncontested dispute and
// its claimant stake.
func seedDispute(t *testing.T, f *fixture, name, owner, claimant string, filedAt int64) {
	t.Helper()
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{Name: name, Owner: owner}))
	require.NoError(t, f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, name)))
	require.NoError(t, f.keeper.SetDispute(f.ctx, types.Dispute{
		Name: name, Claimant: claimant, FiledAt: filedAt, StakeAmount: math.NewInt(50), Active: true,
	}))
	id := fmt.Sprintf("name_dispute:%s:%d", name, filedAt)
	require.NoError(t, f.keeper.DisputeStakes.Set(f.ctx, id, types.DisputeStake{ChallengeId: id, Staker: claimant, Amount: math.NewInt(50)}))
}

// NAM-01: one committee member cannot move the expiry or dispute clocks,
// and even the committee is held inside compiled bounds.
func TestUpdateOperationalParams_PolicyOnlyAndBounded(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	opsPolicy := nameAddr("ops_policy")
	f.mockCommons.Groups["Commons Operations Committee"] = commonstypes.Group{PolicyAddress: opsPolicy}

	op := types.DefaultNameOperationalParams()

	// A bare member key (IsCouncilAuthorized would have accepted it) is refused.
	_, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: nameAddr("ops_member"), OperationalParams: op})
	require.ErrorIs(t, err, types.ErrInvalidSigner)

	for desc, mut := range map[string]func(o *types.NameOperationalParams){
		"instant expiry":       func(o *types.NameOperationalParams) { o.ExpirationDuration = time.Second },
		"one-block dispute":    func(o *types.NameOperationalParams) { o.DisputeTimeoutBlocks = 1 },
		"free disputes":        func(o *types.NameOperationalParams) { o.DisputeStakeDream = math.ZeroInt() },
		"unaffordable contest": func(o *types.NameOperationalParams) { o.ContestStakeDream = math.NewInt(10_000_000_000) },
	} {
		bad := op
		mut(&bad)
		_, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: opsPolicy, OperationalParams: bad})
		require.Error(t, err, desc)
	}

	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: opsPolicy, OperationalParams: op})
	require.NoError(t, err)
}

// NAM-02: dispute resolution is bound to the real committee policy; a group
// that merely carries the name "Operations Committee" is refused, and an
// uncontested dispute cannot be upheld inside the owner's contest window.
func TestResolveDispute_RealCommitteeAndContestWindow(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	owner, claimant := nameAddr("owner"), nameAddr("claimant")
	f.mockRep.ActiveMembers[claimant] = true
	seedDispute(t, f, "phoenix", owner, claimant, 100)

	squatter := nameAddr("squatter_policy")
	f.mockCommons.Groups["Operations Committee"] = commonstypes.Group{PolicyAddress: squatter}
	late := f.ctx.WithBlockHeight(200_000)
	_, err := ms.ResolveDispute(late, &types.MsgResolveDispute{Authority: squatter, Name: "phoenix", TransferApproved: true})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)

	opsPolicy := nameAddr("ops_policy")
	f.mockCommons.Groups["Commons Operations Committee"] = commonstypes.Group{PolicyAddress: opsPolicy}

	early := f.ctx.WithBlockHeight(200)
	_, err = ms.ResolveDispute(early, &types.MsgResolveDispute{Authority: opsPolicy, Name: "phoenix", TransferApproved: true})
	require.ErrorContains(t, err, "contest window")

	_, err = ms.ResolveDispute(late, &types.MsgResolveDispute{Authority: opsPolicy, Name: "phoenix", NewOwner: nameAddr("aurora"), TransferApproved: true})
	require.ErrorContains(t, err, "new_owner")

	_, err = ms.ResolveDispute(late, &types.MsgResolveDispute{Authority: opsPolicy, Name: " PHOENIX ", TransferApproved: true})
	require.NoError(t, err, "names are normalized")
	rec, found := f.keeper.GetName(late, "phoenix")
	require.True(t, found)
	require.Equal(t, claimant, rec.Owner)
}

// NAM-03: an uncontested dispute against an owner who is still active goes
// to council review instead of auto-transferring; against an inactive owner
// it is upheld only for an eligible claimant.
func TestBeginBlocker_UncontestedNeedsReviewOrEligibleClaimant(t *testing.T) {
	f := initFixture(t)
	owner, claimant := nameAddr("owner"), nameAddr("claimant")
	f.mockRep.ActiveMembers[claimant] = true
	seedDispute(t, f, "aurora", owner, claimant, 100)
	require.NoError(t, f.keeper.Owners.Set(f.ctx, owner, types.OwnerInfo{Address: owner, LastActiveTime: f.ctx.BlockTime().Unix()}))

	ctx := f.ctx.WithBlockHeight(200_000)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	d, _ := f.keeper.GetDispute(ctx, "aurora")
	require.True(t, d.Active, "an active owner's name waits for council review")
	rec, _ := f.keeper.GetName(ctx, "aurora")
	require.Equal(t, owner, rec.Owner)
	has, err := f.keeper.PendingDisputes.Has(ctx, collections.Join(int64(100), "aurora"))
	require.NoError(t, err)
	require.False(t, has, "removed from the auto-resolution queue")

	// Inactive owner, but the claimant is no longer an active member: the
	// dispute is voided and the stake returned, no transfer.
	g := initFixture(t)
	seedDispute(t, g, "zenith", owner, claimant, 100)
	ctx2 := g.ctx.WithBlockHeight(200_000)
	require.NoError(t, g.keeper.BeginBlocker(ctx2))
	d2, _ := g.keeper.GetDispute(ctx2, "zenith")
	require.False(t, d2.Active)
	rec2, _ := g.keeper.GetName(ctx2, "zenith")
	require.Equal(t, owner, rec2.Owner)
	require.True(t, g.mockRep.UnlockedDREAM[claimant].Equal(math.NewInt(50)))
}

// NAM-04: a failing auto-resolution leaves no half-applied writes and closes
// the dispute instead of retrying every block. When the stake cannot be
// returned either, the stake accounting is kept: the dispute stays active
// with its DisputeStakes row, leaves the queue, and the council path
// releases it later.
func TestBeginBlocker_FailureIsAtomic(t *testing.T) {
	f := initFixture(t)
	owner, claimant := nameAddr("owner"), nameAddr("claimant")
	f.mockRep.ActiveMembers[claimant] = true
	seedDispute(t, f, "nova", owner, claimant, 100)
	f.mockRep.unlockErr = errors.New("claimant zeroed")

	ctx := f.ctx.WithBlockHeight(200_000)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	rec, _ := f.keeper.GetName(ctx, "nova")
	require.Equal(t, owner, rec.Owner, "the transfer written before the failing unlock is rolled back")
	d, _ := f.keeper.GetDispute(ctx, "nova")
	require.True(t, d.Active, "the still-locked stake keeps the dispute open")
	stakeID := "name_dispute:nova:100"
	has, err := f.keeper.DisputeStakes.Has(ctx, stakeID)
	require.NoError(t, err)
	require.True(t, has, "the DisputeStakes row for still-locked DREAM is never dropped")
	has, err = f.keeper.PendingDisputes.Has(ctx, collections.Join(int64(100), "nova"))
	require.NoError(t, err)
	require.False(t, has, "not retried every block")
	_, broken := keeper.DisputeStakesInvariant(f.keeper)(ctx)
	require.False(t, broken)

	// A second block does nothing.
	require.NoError(t, f.keeper.BeginBlocker(ctx.WithBlockHeight(200_001)))

	// Once the cause is gone, the council path releases the stake.
	f.mockRep.unlockErr = nil
	opsPolicy := nameAddr("ops_policy")
	f.mockCommons.Groups["Commons Operations Committee"] = commonstypes.Group{PolicyAddress: opsPolicy}
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err = ms.ResolveDispute(ctx, &types.MsgResolveDispute{Authority: opsPolicy, Name: "nova", TransferApproved: true})
	require.NoError(t, err)
	d, _ = f.keeper.GetDispute(ctx, "nova")
	require.False(t, d.Active)
	require.True(t, f.mockRep.UnlockedDREAM[claimant].Equal(math.NewInt(50)))
	has, err = f.keeper.DisputeStakes.Has(ctx, stakeID)
	require.NoError(t, err)
	require.False(t, has)
}

// NAM-04: when only the resolution fails and the stake can be returned, the
// dispute is closed and its stake row removed.
func TestBeginBlocker_FailureClosesWhenStakeReturned(t *testing.T) {
	f := initFixture(t)
	owner, claimant := nameAddr("owner"), nameAddr("claimant")
	f.mockRep.ActiveMembers[claimant] = true
	seedDispute(t, f, "lumen", owner, claimant, 100)
	// The unlock inside the resolution fails; the compensating one works.
	f.mockRep.unlockFailures = 1

	ctx := f.ctx.WithBlockHeight(200_000)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	d, _ := f.keeper.GetDispute(ctx, "lumen")
	require.False(t, d.Active)
	require.True(t, f.mockRep.UnlockedDREAM[claimant].Equal(math.NewInt(50)))
	has, err := f.keeper.DisputeStakes.Has(ctx, "name_dispute:lumen:100")
	require.NoError(t, err)
	require.False(t, has)
	rec, _ := f.keeper.GetName(ctx, "lumen")
	require.Equal(t, owner, rec.Owner)
}

// NAM-05: the migration hook finds targeted names through the index.
func TestMigrationHooks_TargetIndex(t *testing.T) {
	f := initFixture(t)
	from, to := sdk.AccAddress([]byte("from_address________")), sdk.AccAddress([]byte("to_address__________"))
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{Name: "comet", Owner: nameAddr("other"), Target: from.String()}))
	h := keeper.NewMigrationHooks(&f.keeper)
	require.NoError(t, h.AfterMemberMigration(f.ctx, from, to, 0))
	rec, _ := f.keeper.GetName(f.ctx, "comet")
	require.Equal(t, to.String(), rec.Target)
	has, err := f.keeper.TargetNames.Has(f.ctx, collections.Join(to.String(), "comet"))
	require.NoError(t, err)
	require.True(t, has)
	has, err = f.keeper.TargetNames.Has(f.ctx, collections.Join(from.String(), "comet"))
	require.NoError(t, err)
	require.False(t, has)
}

// NAM-09: a name under an active dispute is not scavengeable.
func TestRegisterName_NoScavengeUnderDispute(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	owner, claimant, scavenger := nameAddr("owner"), nameAddr("claimant"), nameAddr("scavenger")
	f.mockRep.ActiveMembers[scavenger] = true
	seedDispute(t, f, "orbit", owner, claimant, 100)
	// The owner has been inactive for ten years.
	require.NoError(t, f.keeper.Owners.Set(f.ctx, owner, types.OwnerInfo{Address: owner, LastActiveTime: f.ctx.BlockTime().Add(-10 * 365 * 24 * time.Hour).Unix()}))

	_, err := ms.RegisterName(f.ctx, &types.MsgRegisterName{Authority: scavenger, Name: "orbit"})
	require.ErrorIs(t, err, types.ErrNameTaken)
}

// NAM-07: blocked names match case-insensitively; a contested dispute
// without its contest stake fails explicitly.
func TestNameLeftovers(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	alice := nameAddr("alice")
	f.mockRep.ActiveMembers[alice] = true
	p := types.DefaultParams()
	p.BlockedNames = append(p.BlockedNames, "Vesper")
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
	_, err := ms.RegisterName(f.ctx, &types.MsgRegisterName{Authority: alice, Name: "vesper"})
	require.ErrorIs(t, err, types.ErrNameReserved)

	_, err = ms.RegisterName(f.ctx, &types.MsgRegisterName{Authority: alice, Name: "lumen", Data: string(make([]byte, keeper.MaxNameDataLen+1))})
	require.ErrorIs(t, err, types.ErrInvalidName)

	owner, claimant := nameAddr("owner"), nameAddr("claimant")
	seedDispute(t, f, "halo", owner, claimant, 100)
	d, _ := f.keeper.GetDispute(f.ctx, "halo")
	d.ContestChallengeId = "name_contest:halo:150"
	require.NoError(t, f.keeper.SetDispute(f.ctx, d))
	f.mockCommons.Groups["Commons Council"] = commonstypes.Group{PolicyAddress: nameAddr("council")}
	_, err = ms.ResolveDispute(f.ctx, &types.MsgResolveDispute{Authority: nameAddr("council"), Name: "halo"})
	require.ErrorContains(t, err, "contest stake")

	gs := types.DefaultGenesis()
	gs.DisputeMap = []types.Dispute{d}
	require.Error(t, gs.Validate(), "an active contested dispute needs its contest stake row")
}

// NAM-10: min_name_length must not exceed max_name_length.
func TestParams_MinNotAboveMax(t *testing.T) {
	p := types.DefaultParams()
	p.MinNameLength, p.MaxNameLength = 10, 5
	require.Error(t, p.Validate())
}

// NAM-10: a display-name-only owner record does not block a migration onto
// that address.
func TestMigrationHooks_DisplayNameOnlyTargetIsFresh(t *testing.T) {
	f := initFixture(t)
	from, to := sdk.AccAddress([]byte("from_address________")), sdk.AccAddress([]byte("to_address__________"))
	require.NoError(t, f.keeper.Owners.Set(f.ctx, to.String(), types.OwnerInfo{Address: to.String(), DisplayName: "Nova"}))
	h := keeper.NewMigrationHooks(&f.keeper)
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, 0))

	require.NoError(t, f.keeper.Owners.Set(f.ctx, to.String(), types.OwnerInfo{Address: to.String(), PrimaryName: "nova"}))
	require.Error(t, h.BeforeMemberMigration(f.ctx, from, to, 0))
}
