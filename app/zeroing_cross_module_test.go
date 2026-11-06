package app

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	nametypes "sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"
	revealkeeper "sparkdream/x/reveal/keeper"
	revealtypes "sparkdream/x/reveal/types"
	seasontypes "sparkdream/x/season/types"
)

// TestZeroingClosesEveryModulesLocks zeroes a member who holds DREAM locks in
// x/season (display-name report and appeal stakes) and x/reveal (a stake on
// a BACKED tranche and a contribution bond) through the real x/rep keeper and
// wired zeroing hooks, then drives each resolution path (review theme 1):
//
//   - every season resolution path closes its case instead of failing on the
//     zeroed side's lock (SEA-101);
//   - the member's own contribution is cancelled and pays nothing (REV-102);
//   - after the zeroing is overturned and the member locks DREAM elsewhere,
//     settling the reveal tranche leaves that unrelated lock alone (REV-101);
//   - x/name: the dispute the member filed is closed and the contesting
//     owner repaid (NAM-101); the dispute on the member's own name is
//     decided at once, upheld for the eligible claimant (NAM-202), so
//     nothing is left to touch the lock taken after the overturn.
func TestZeroingClosesEveryModulesLocks(t *testing.T) {
	files := snapshotGenesisFiles(t)
	if len(files) == 0 {
		t.Skip("no e2e snapshot; run /save-setup")
	}
	app, ctx, err := newAppFromGenesisFile(t, files[0])
	if err != nil {
		t.Skipf("snapshot %s does not import (%v); re-run /save-setup", files[0], err)
	}

	zeroed := sdk.AccAddress([]byte("zeroing-guard-member"))
	other := sdk.AccAddress([]byte("zeroing-guard-other_"))
	t1 := sdk.AccAddress([]byte("zeroing-guard-t1____"))
	t2 := sdk.AccAddress([]byte("zeroing-guard-t2____"))
	n1 := sdk.AccAddress([]byte("zeroing-guard-n1____"))
	n2 := sdk.AccAddress([]byte("zeroing-guard-n2____"))
	for _, a := range []sdk.AccAddress{zeroed, other, t1, t2, n1, n2} {
		bal := sdkmath.NewInt(100_000)
		zero := sdkmath.ZeroInt()
		zero2, zero3 := sdkmath.ZeroInt(), sdkmath.ZeroInt()
		require.NoError(t, app.RepKeeper.Member.Set(ctx, a.String(), reptypes.Member{
			Address:        a.String(),
			Status:         reptypes.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   &bal,
			StakedDream:    &zero,
			LifetimeEarned: &zero2,
			LifetimeBurned: &zero3,
			TrustLevel:     reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED,
			ContentLicense: "CC0-1.0",
		}))
	}
	lock := func(a sdk.AccAddress, n int64) {
		require.NoError(t, app.RepKeeper.LockDREAM(ctx, a, sdkmath.NewInt(n)))
	}
	staked := func(a sdk.AccAddress) sdkmath.Int {
		m, err := app.RepKeeper.Member.Get(ctx, a.String())
		require.NoError(t, err)
		return *m.StakedDream
	}

	// --- x/season: three cases the member is a party to. ---
	sk := app.SeasonKeeper
	h := ctx.BlockHeight()
	moderate := func(target, reporter sdk.AccAddress, appeal bool) {
		reportID := fmt.Sprintf("dn:%s:%d", target, h)
		m := seasontypes.DisplayNameModeration{Member: target.String(), RejectedName: "Rogue", ModeratedAt: h, Active: true}
		if appeal {
			m.AppealChallengeId = "dna:" + target.String()
			m.AppealedAt = h
			lock(target, 50)
			require.NoError(t, sk.DisplayNameAppealStake.Set(ctx, m.AppealChallengeId, seasontypes.DisplayNameAppealStake{
				ChallengeId: m.AppealChallengeId, Appellant: target.String(), Amount: sdkmath.NewInt(50),
			}))
		}
		lock(reporter, 50)
		require.NoError(t, sk.DisplayNameReportStake.Set(ctx, reportID, seasontypes.DisplayNameReportStake{
			ChallengeId: reportID, Reporter: reporter.String(), Amount: sdkmath.NewInt(50),
		}))
		require.NoError(t, sk.SetDisplayNameModeration(ctx, target.String(), m))
	}
	moderate(t1, zeroed, false)   // reported by the member, unappealed
	moderate(t2, zeroed, true)    // reported by the member, appealed by t2
	moderate(zeroed, other, true) // the member's own appeal

	// --- x/reveal: the member stakes on other's BACKED tranche and has
	// a contribution of their own in progress. ---
	rk := app.RevealKeeper
	contribution := func(id uint64, contributor sdk.AccAddress, tranche revealtypes.TrancheStatus, bond int64) {
		lock(contributor, bond)
		c := revealtypes.Contribution{
			Id: id, Contributor: contributor.String(), ProjectName: fmt.Sprintf("zenith-%d", id),
			TotalValuation: sdkmath.NewInt(1000), BondAmount: sdkmath.NewInt(bond), BondRemaining: sdkmath.NewInt(bond),
			HoldbackAmount: sdkmath.ZeroInt(), Status: revealtypes.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS,
			Tranches: []revealtypes.RevealTranche{{
				Id: 0, Name: "all", StakeThreshold: sdkmath.NewInt(1000), DreamStaked: sdkmath.ZeroInt(),
				Status: tranche, StakeDeadline: h + 1000, RevealDeadline: h + 1000,
			}},
		}
		require.NoError(t, rk.SetContribution(ctx, c))
		require.NoError(t, rk.ContributionsByStatus.Set(ctx, collections.Join(int32(c.Status), id)))
		require.NoError(t, rk.ContributionsByContributor.Set(ctx, collections.Join(c.Contributor, id)))
	}
	stake := func(id, contrib uint64, staker sdk.AccAddress, n int64) {
		lock(staker, n)
		require.NoError(t, rk.RevealStake.Set(ctx, id, revealtypes.RevealStake{
			Id: id, Staker: staker.String(), ContributionId: contrib, TrancheId: 0, Amount: sdkmath.NewInt(n),
		}))
		require.NoError(t, rk.StakesByTranche.Set(ctx, collections.Join(revealkeeper.TrancheKey(contrib, 0), id)))
		require.NoError(t, rk.StakesByStaker.Set(ctx, collections.Join(staker.String(), id)))
	}
	const othersID, ownID = 900001, 900002
	contribution(othersID, other, revealtypes.TrancheStatus_TRANCHE_STATUS_BACKED, 100)
	stake(900001, othersID, zeroed, 500)
	stake(900002, othersID, t1, 500)
	contribution(ownID, zeroed, revealtypes.TrancheStatus_TRANCHE_STATUS_STAKING, 100)
	stake(900003, ownID, t2, 300)

	// --- x/name: the member disputes n1's name (n1 contests) and n2
	// disputes the member's name (the member contests). ---
	nk := app.NameKeeper
	dispute := func(name string, owner, claimant sdk.AccAddress) {
		require.NoError(t, nk.SetName(ctx, nametypes.NameRecord{Name: name, Owner: owner.String()}))
		require.NoError(t, nk.OwnerNames.Set(ctx, collections.Join(owner.String(), name)))
		lock(claimant, 50)
		lock(owner, 100)
		disputeID := fmt.Sprintf("name_dispute:%s:%d", name, h)
		contestID := fmt.Sprintf("name_contest:%s:%d", name, h)
		require.NoError(t, nk.SetDispute(ctx, nametypes.Dispute{
			Name: name, Claimant: claimant.String(), FiledAt: h, StakeAmount: sdkmath.NewInt(50),
			Active: true, ContestChallengeId: contestID, ContestedAt: h,
		}))
		require.NoError(t, nk.DisputeStakes.Set(ctx, disputeID, nametypes.DisputeStake{
			ChallengeId: disputeID, Staker: claimant.String(), Amount: sdkmath.NewInt(50),
		}))
		require.NoError(t, nk.ContestStakes.Set(ctx, contestID, nametypes.ContestStake{
			ChallengeId: contestID, Owner: owner.String(), Amount: sdkmath.NewInt(100),
		}))
	}
	dispute("zgaurora", n1, zeroed)
	dispute("zgzenith", zeroed, n2)

	// --- Zero the member through x/rep: every module's hook runs. ---
	require.NoError(t, app.RepKeeper.ZeroMember(ctx, zeroed, "cross-module zeroing guard"))
	require.True(t, staked(zeroed).IsZero())

	// x/season: the member's stakes are gone; every resolution path closes.
	late := ctx.WithBlockHeight(h + 1_000_000_000)
	require.NoError(t, sk.ResolveUnappealedModerationInternal(late, t1.String()))
	require.NoError(t, sk.ResolveDisplayNameAppealInternal(ctx, t2.String(), true))
	require.NoError(t, sk.ResolveAppealTimeoutInternal(late, zeroed.String()))
	for _, target := range []sdk.AccAddress{t1, t2, zeroed} {
		m, err := sk.DisplayNameModeration.Get(late, target.String())
		require.NoError(t, err)
		require.False(t, m.Active, "case of %s closed", target)
	}
	require.True(t, staked(t2).IsZero(), "t2's appeal stake returned")
	require.True(t, staked(other).Equal(sdkmath.NewInt(100)), "other's report stake returned, bond still locked")

	// x/reveal: the member's own contribution is cancelled, t2 is repaid.
	own, err := rk.Contribution.Get(ctx, ownID)
	require.NoError(t, err)
	require.Equal(t, revealtypes.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, own.Status)
	require.True(t, staked(t2).IsZero())
	has, err := rk.RevealStake.Has(ctx, 900001)
	require.NoError(t, err)
	require.False(t, has, "the member's BACKED-tranche stake is dropped")

	// x/name: the member's own dispute is closed and n1 repaid; the
	// dispute on the member's name is upheld for n2, who is repaid.
	d, found := nk.GetDispute(ctx, "zgaurora")
	require.True(t, found)
	require.False(t, d.Active, "the zeroed claimant's dispute is closed")
	require.True(t, staked(n1).IsZero(), "n1's contest stake returned")
	d, found = nk.GetDispute(ctx, "zgzenith")
	require.True(t, found)
	require.False(t, d.Active, "the dispute against the zeroed owner is decided")
	rec, found := nk.GetName(ctx, "zgzenith")
	require.True(t, found)
	require.Equal(t, n2.String(), rec.Owner, "the eligible claimant receives the name")
	require.True(t, staked(n2).IsZero(), "n2's dispute stake returned")

	// --- Overturn: x/rep re-mints the burn as liquid DREAM; the member
	// locks some of it elsewhere. ---
	m, err := app.RepKeeper.Member.Get(ctx, zeroed.String())
	require.NoError(t, err)
	m.Status = reptypes.MemberStatus_MEMBER_STATUS_ACTIVE
	restored := m.DreamBalance.Add(sdkmath.NewInt(100_000))
	m.DreamBalance = &restored
	require.NoError(t, app.RepKeeper.Member.Set(ctx, zeroed.String(), m))
	lock(zeroed, 500)

	// Settling other's tranche returns t1's stake and nothing of the member's.
	gov := app.RevealKeeper.GetAuthorityString()
	_, err = revealkeeper.NewMsgServerImpl(rk).Cancel(ctx, &revealtypes.MsgCancel{Authority: gov, ContributionId: othersID, Reason: "guard"})
	require.NoError(t, err)
	require.True(t, staked(zeroed).Equal(sdkmath.NewInt(500)), "the member's unrelated lock is intact")
	require.True(t, staked(t1).IsZero(), "t1's reveal stake returned")
}
