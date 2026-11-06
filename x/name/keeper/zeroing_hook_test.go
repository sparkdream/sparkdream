package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// seedContestedDispute stores name owned by owner, disputed by claimant
// (stake 50) and contested by the owner (stake 100).
func seedContestedDispute(t *testing.T, f *fixture, name, owner, claimant string) {
	t.Helper()
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{Name: name, Owner: owner}))
	require.NoError(t, f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, name)))
	contestID := fmt.Sprintf("name_contest:%s:%d", name, 150)
	require.NoError(t, f.keeper.SetDispute(f.ctx, types.Dispute{
		Name: name, Claimant: claimant, FiledAt: 100, StakeAmount: math.NewInt(50),
		Active: true, ContestChallengeId: contestID, ContestedAt: 150,
	}))
	disputeID := fmt.Sprintf("name_dispute:%s:%d", name, 100)
	require.NoError(t, f.keeper.DisputeStakes.Set(f.ctx, disputeID, types.DisputeStake{
		ChallengeId: disputeID, Staker: claimant, Amount: math.NewInt(50),
	}))
	require.NoError(t, f.keeper.ContestStakes.Set(f.ctx, contestID, types.ContestStake{
		ChallengeId: contestID, Owner: owner, Amount: math.NewInt(100),
	}))
}

func unlocked(f *fixture, addr string) math.Int {
	if v, ok := f.mockRep.UnlockedDREAM[addr]; ok {
		return v
	}
	return math.ZeroInt()
}

func burned(f *fixture, addr string) math.Int {
	if v, ok := f.mockRep.BurnedDREAM[addr]; ok {
		return v
	}
	return math.ZeroInt()
}

// NAM-101: a zeroed claimant's dispute is closed by the zeroing hook: the
// contesting owner gets their stake back and the name is no longer frozen.
func TestNAM101_ZeroedClaimantClosesDispute(t *testing.T) {
	f := initFixture(t)
	owner := sdk.AccAddress([]byte("owner_address_______")).String()
	claimant := sdk.AccAddress([]byte("claimant_address____")).String()
	seedContestedDispute(t, f, "aurora", owner, claimant)

	f.mockRep.Gone = map[string]bool{claimant: true}
	require.NoError(t, keeper.NewZeroingHook(&f.keeper).AfterMemberZeroed(f.ctx, claimant, []string{claimant}))

	d, found := f.keeper.GetDispute(f.ctx, "aurora")
	require.True(t, found)
	require.False(t, d.Active)
	require.True(t, d.ContestSucceeded)
	require.True(t, unlocked(f, owner).Equal(math.NewInt(100)), "owner's contest stake returned")
	has, err := f.keeper.ContestStakes.Has(f.ctx, d.ContestChallengeId)
	require.NoError(t, err)
	require.False(t, has)
	has, err = f.keeper.DisputeStakes.Has(f.ctx, "name_dispute:aurora:100")
	require.NoError(t, err)
	require.False(t, has)
	has, err = f.keeper.ClaimantDisputes.Has(f.ctx, collections.Join(claimant, "aurora"))
	require.NoError(t, err)
	require.False(t, has, "claimant index cleared")
	rec, _ := f.keeper.GetName(f.ctx, "aurora")
	require.Equal(t, owner, rec.Owner)

	// Invariants hold on the closed dispute.
	_, broken := keeper.DisputeStakesInvariant(f.keeper)(f.ctx)
	require.False(t, broken)
}

// NAM-202: a dispute against a zeroed owner is decided by the hook: upheld
// (name transferred, claimant stake returned) when the claimant can still
// receive the name, voided (claimant stake returned, name stays) when not.
// The zeroed owner's contest stake is gone with the balance burn, and every
// name the owner keeps is released for scavenging.
func TestNAM202_ZeroedOwnerDisputeDecided(t *testing.T) {
	for _, eligible := range []bool{true, false} {
		t.Run(fmt.Sprintf("claimant_eligible=%t", eligible), func(t *testing.T) {
			f := initFixture(t)
			owner := sdk.AccAddress([]byte("owner_address_______")).String()
			claimant := sdk.AccAddress([]byte("claimant_address____")).String()
			if eligible {
				f.mockRep.SetActiveMember(claimant)
			}
			seedContestedDispute(t, f, "zenith", owner, claimant)
			require.NoError(t, f.keeper.Owners.Set(f.ctx, owner, types.OwnerInfo{
				Address: owner, PrimaryName: "zenith", LastActiveTime: f.ctx.BlockTime().Unix(),
			}))

			f.mockRep.Gone = map[string]bool{owner: true}
			require.NoError(t, keeper.NewZeroingHook(&f.keeper).AfterMemberZeroed(f.ctx, owner, []string{owner}))

			d, _ := f.keeper.GetDispute(f.ctx, "zenith")
			require.False(t, d.Active, "decided by the hook")
			has, err := f.keeper.ContestStakes.Has(f.ctx, "name_contest:zenith:150")
			require.NoError(t, err)
			require.False(t, has, "contest stake record dropped")
			has, err = f.keeper.DisputeStakes.Has(f.ctx, "name_dispute:zenith:100")
			require.NoError(t, err)
			require.False(t, has)
			require.True(t, unlocked(f, owner).IsZero(), "nothing released for the zeroed owner")
			require.True(t, unlocked(f, claimant).Equal(math.NewInt(50)), "claimant stake returned")
			require.True(t, burned(f, claimant).IsZero())
			rec, _ := f.keeper.GetName(f.ctx, "zenith")
			if eligible {
				require.Equal(t, claimant, rec.Owner, "upheld: name transferred")
			} else {
				require.Equal(t, owner, rec.Owner, "voided: name stays")
			}
			info, err := f.keeper.Owners.Get(f.ctx, owner)
			require.NoError(t, err)
			require.True(t, info.Scavengeable, "owner released")
			require.True(t, f.keeper.IsOwnerExpired(f.ctx, sdk.MustAccAddressFromBech32(owner)))
			_, broken := keeper.DisputeStakesInvariant(f.keeper)(f.ctx)
			require.False(t, broken)
		})
	}
}

// NAM-202: a zeroed owner's names are scavengeable at once, whatever the
// activity clock says, and cannot be handed to a successor.
func TestNAM202_ZeroedOwnerNamesReleased(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	owner := sdk.AccAddress([]byte("owner_address_______")).String()
	friend := sdk.AccAddress([]byte("friend_address______")).String()
	f.mockRep.SetActiveMember(friend)
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{Name: "aurora", Owner: owner}))
	require.NoError(t, f.keeper.AddNameToOwner(f.ctx, sdk.MustAccAddressFromBech32(owner), "aurora"))
	ownerAddr := sdk.MustAccAddressFromBech32(owner)
	require.False(t, f.keeper.IsOwnerExpired(f.ctx, ownerAddr), "active before the zeroing")

	require.NoError(t, keeper.NewZeroingHook(&f.keeper).AfterMemberZeroed(f.ctx, owner, []string{owner}))
	require.True(t, f.keeper.IsOwnerExpired(f.ctx, ownerAddr), "released by the zeroing")

	// Owner activity after the zeroing does not revive the names.
	require.NoError(t, f.keeper.RecordOwnerActivity(f.ctx, owner))
	require.True(t, f.keeper.IsOwnerExpired(f.ctx, ownerAddr))

	_, err := ms.TransferName(f.ctx, &types.MsgTransferName{Authority: owner, Name: "aurora", NewOwner: friend})
	require.ErrorIs(t, err, types.ErrNotAuthorized, "released names cannot be transferred away")
	rec, _ := f.keeper.GetName(f.ctx, "aurora")
	require.Equal(t, owner, rec.Owner)
}

// NAM-201: a contested dispute the council leaves undecided is dismissed
// at the end of the review window, exactly like a dismissal: the owner
// keeps the name and the contest stake, the claimant's stake is burned.
func TestNAM201_ContestedDisputeDismissedAfterReviewWindow(t *testing.T) {
	f := initFixture(t)
	owner := sdk.AccAddress([]byte("owner_address_______")).String()
	claimant := sdk.AccAddress([]byte("claimant_address____")).String()
	seedContestedDispute(t, f, "phoenix", owner, claimant)
	timeout := int64(f.keeper.GetParams(f.ctx).DisputeTimeoutBlocks)
	has, err := f.keeper.ReviewDisputes.Has(f.ctx, collections.Join(int64(100), "phoenix"))
	require.NoError(t, err)
	require.True(t, has, "a contested dispute is queued for the review window")

	ctx := f.ctx.WithBlockHeight(100 + 2*timeout)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	d, _ := f.keeper.GetDispute(ctx, "phoenix")
	require.True(t, d.Active, "still the council's until the window closes")

	ctx = f.ctx.WithBlockHeight(100 + 2*timeout + 1)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	d, _ = f.keeper.GetDispute(ctx, "phoenix")
	require.False(t, d.Active, "dismissed after the review window")
	require.True(t, d.ContestSucceeded)
	require.True(t, unlocked(f, owner).Equal(math.NewInt(100)), "owner's contest stake returned")
	require.True(t, burned(f, claimant).Equal(math.NewInt(50)), "claimant stake burned")
	rec, _ := f.keeper.GetName(ctx, "phoenix")
	require.Equal(t, owner, rec.Owner)
	has, err = f.keeper.ContestStakes.Has(ctx, d.ContestChallengeId)
	require.NoError(t, err)
	require.False(t, has)
	has, err = f.keeper.ReviewDisputes.Has(ctx, collections.Join(int64(100), "phoenix"))
	require.NoError(t, err)
	require.False(t, has)
	_, broken := keeper.DisputeStakesInvariant(f.keeper)(ctx)
	require.False(t, broken)
}

// NAM-101 fallback: without the hook (e.g. it failed), a party whose lock
// is gone no longer wedges resolution.
func TestNAM101_GoneLockDoesNotWedgeResolution(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	gov, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	owner := sdk.AccAddress([]byte("owner_address_______")).String()
	claimant := sdk.AccAddress([]byte("claimant_address____")).String()
	seedContestedDispute(t, f, "phoenix", owner, claimant)

	f.mockRep.Gone = map[string]bool{claimant: true}
	_, err := ms.ResolveDispute(f.ctx, &types.MsgResolveDispute{Authority: gov, Name: "phoenix", TransferApproved: false})
	require.NoError(t, err)
	d, _ := f.keeper.GetDispute(f.ctx, "phoenix")
	require.False(t, d.Active)
	require.True(t, unlocked(f, owner).Equal(math.NewInt(100)), "owner's stake returned")
}

// NAM-102: a dispute the council leaves awaiting review is dismissed one
// review window (dispute_timeout_blocks) after the contest window.
func TestNAM102_UnreviewedDisputeDismissed(t *testing.T) {
	f := initFixture(t)
	owner := sdk.AccAddress([]byte("owner_address_______")).String()
	claimant := sdk.AccAddress([]byte("claimant_address____")).String()
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{Name: "nova", Owner: owner}))
	require.NoError(t, f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, "nova")))
	require.NoError(t, f.keeper.SetDispute(f.ctx, types.Dispute{
		Name: "nova", Claimant: claimant, FiledAt: 100, StakeAmount: math.NewInt(50),
		Active: true, AwaitingReview: true,
	}))
	require.NoError(t, f.keeper.DisputeStakes.Set(f.ctx, "name_dispute:nova:100", types.DisputeStake{
		ChallengeId: "name_dispute:nova:100", Staker: claimant, Amount: math.NewInt(50),
	}))
	timeout := int64(f.keeper.GetParams(f.ctx).DisputeTimeoutBlocks)

	// At the end of the review window the dispute is still open.
	ctx := f.ctx.WithBlockHeight(100 + 2*timeout)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	d, _ := f.keeper.GetDispute(ctx, "nova")
	require.True(t, d.Active)

	ctx = f.ctx.WithBlockHeight(100 + 2*timeout + 1)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	d, _ = f.keeper.GetDispute(ctx, "nova")
	require.False(t, d.Active, "dismissed after the review window")
	require.True(t, burned(f, claimant).Equal(math.NewInt(50)), "claimant stake burned as on a dismissal")
	rec, _ := f.keeper.GetName(ctx, "nova")
	require.Equal(t, owner, rec.Owner)
	has, err := f.keeper.ReviewDisputes.Has(ctx, collections.Join(int64(100), "nova"))
	require.NoError(t, err)
	require.False(t, has)
}
