package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// juryAppealFixture wires a fixture with a seated appeal jury: a sentinel for a
// THREAD_LOCK action, JurySize eligible juror members, an appellant, and an
// appeal filed through the real AppealGovAction flow (which now seats a jury).
type juryAppealFixture struct {
	f             *fixture
	fk            *mockForumKeeper
	appellantStr  string
	sentinel      string
	actionType    types.GovActionType
	actionTarget  string
	appealID      uint64
	review        types.JuryReview
	refundedCoins sdk.Coins
	burnedCoins   sdk.Coins
}

func mkJurorMember(t *testing.T, f *fixture, label string, rep string) sdk.AccAddress {
	t.Helper()
	addr := sdk.AccAddress([]byte(fmt.Sprintf("%-20.20s", label)))
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:          addr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     keeper.PtrInt(math.NewInt(1000)),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.NewInt(1000)),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"coding": rep},
	}))
	return addr
}

func setupJuryAppeal(t *testing.T) *juryAppealFixture {
	t.Helper()

	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	fk := &mockForumKeeper{
		authors:         make(map[uint64]string),
		tags:            make(map[uint64][]string),
		actionSentinels: make(map[string]string),
	}
	f.keeper.SetForumKeeper(fk)

	ja := &juryAppealFixture{
		f:            f,
		fk:           fk,
		actionType:   types.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK,
		actionTarget: "1",
		sentinel:     sdk.AccAddress([]byte(fmt.Sprintf("%-20.20s", "jury_sentinel"))).String(),
	}

	// Observe refunds (escrow -> appellant) and burns.
	escrowAddr := keeper.AppealBondEscrowAddress()
	repModuleAddr := authtypes.NewModuleAddress(types.ModuleName)
	f.bankKeeper.SendCoinsFn = func(_ context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
		if !fromAddr.Equals(escrowAddr) {
			return nil
		}
		if toAddr.Equals(repModuleAddr) || toAddr.Equals(keeper.SentinelRewardPoolAddress()) {
			return nil
		}
		ja.refundedCoins = ja.refundedCoins.Add(amt...)
		return nil
	}
	f.bankKeeper.BurnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		ja.burnedCoins = ja.burnedCoins.Add(amt...)
		return nil
	}

	// Sentinel that performed the action (excluded from the jury, slashed on overturn).
	fk.actionSentinels[mockForumKey(ja.actionType, ja.actionTarget)] = ja.sentinel
	seedSlashableSentinel(t, f, ja.sentinel, math.NewInt(1_000_000_000))

	// Seat enough eligible jurors (JurySize default 5).
	for i := 0; i < 6; i++ {
		mkJurorMember(t, f, fmt.Sprintf("jury_juror_%02d", i), "100.0")
	}

	// Appellant files the appeal — CreateGovActionAppeal seats the jury.
	appellant := sdk.AccAddress([]byte(fmt.Sprintf("%-20.20s", "jury_appellant")))
	setActiveMember(t, f.keeper, f.ctx, appellant)
	ja.appellantStr = appellant.String()

	_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
		Creator:      ja.appellantStr,
		ActionType:   uint64(ja.actionType),
		ActionTarget: ja.actionTarget,
		AppealReason: "jury path",
	})
	require.NoError(t, err)

	appealID, appeal := findAppeal(t, f, ja.appellantStr)
	ja.appealID = appealID
	review, err := f.keeper.GetJuryReview(f.ctx, appeal.InitiativeId)
	require.NoError(t, err)
	ja.review = review
	return ja
}

// vote casts `count` juror votes of the given verdict, drawing jurors from the
// seated review in order. The final vote (reaching RequiredVotes) triggers the
// tally + dispatch.
func (ja *juryAppealFixture) vote(t *testing.T, count int, verdict types.Verdict) {
	t.Helper()
	require.LessOrEqual(t, count, len(ja.review.Jurors), "not enough seated jurors")
	for i := 0; i < count; i++ {
		jurorAddr, err := sdk.AccAddressFromBech32(ja.review.Jurors[i])
		require.NoError(t, err)
		require.NoError(t, ja.f.keeper.SubmitJurorVote(
			ja.f.ctx, ja.review.Id, jurorAddr, nil, verdict,
			math.LegacyMustNewDecFromStr("0.9"), "vote",
		))
	}
}

func TestJuryDrivenGovActionAppeal(t *testing.T) {
	t.Run("jury is seated at appeal creation, excluding the parties", func(t *testing.T) {
		ja := setupJuryAppeal(t)

		params, err := ja.f.keeper.Params.Get(ja.f.ctx)
		require.NoError(t, err)
		require.NotEmpty(t, ja.review.Jurors, "a jury must be seated")
		require.Equal(t, int(params.JurySize), len(ja.review.Jurors))
		require.Greater(t, ja.review.RequiredVotes, uint32(0))

		// Neither the appellant nor the accused sentinel may sit on the jury.
		for _, j := range ja.review.Jurors {
			require.NotEqual(t, ja.appellantStr, j)
			require.NotEqual(t, ja.sentinel, j)
		}
	})

	t.Run("supermajority UPHOLD_CHALLENGE overturns the action", func(t *testing.T) {
		ja := setupJuryAppeal(t)

		ja.vote(t, int(ja.review.RequiredVotes), types.Verdict_VERDICT_UPHOLD_CHALLENGE)

		_, updated := findAppeal(t, ja.f, ja.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED, updated.Status)

		// Full bond refunded; sentinel slashed; content reversed.
		require.Equal(t,
			math.NewInt(types.DefaultAppealBondAmount).String(),
			ja.refundedCoins.AmountOf("uspark").String())
		require.True(t, ja.burnedCoins.IsZero())
		require.Len(t, ja.fk.overturnedCalls, 1)
		require.Len(t, ja.fk.reverseCalls, 1)
		require.Empty(t, ja.fk.upheldCalls)

		br, err := ja.f.keeper.BondedRoles.Get(ja.f.ctx, collections.Join(int32(types.RoleType_ROLE_TYPE_FORUM_SENTINEL), ja.sentinel))
		require.NoError(t, err)
		require.Equal(t,
			math.NewInt(1_000_000_000).SubRaw(types.DefaultSentinelOverturnSlash).String(),
			br.CurrentBond)
	})

	t.Run("supermajority REJECT_CHALLENGE upholds the action", func(t *testing.T) {
		ja := setupJuryAppeal(t)

		ja.vote(t, int(ja.review.RequiredVotes), types.Verdict_VERDICT_REJECT_CHALLENGE)

		_, updated := findAppeal(t, ja.f, ja.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD, updated.Status)

		// Half burned, no refund; upheld hook fired; no content reversal.
		require.Equal(t,
			math.NewInt(types.DefaultAppealBondAmount).QuoRaw(2).String(),
			ja.burnedCoins.AmountOf("uspark").String())
		require.True(t, ja.refundedCoins.IsZero())
		require.Len(t, ja.fk.upheldCalls, 1)
		require.Empty(t, ja.fk.overturnedCalls)
		require.Empty(t, ja.fk.reverseCalls)
	})

	t.Run("zero-vote tally leaves the appeal PENDING (no auto-resolution)", func(t *testing.T) {
		ja := setupJuryAppeal(t)

		// Force a tally with no votes cast — the no-quorum guard must keep the
		// appeal PENDING rather than auto-overturning it.
		require.NoError(t, ja.f.keeper.TallyJuryVotes(ja.f.ctx, ja.review.Id))

		_, updated := findAppeal(t, ja.f, ja.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING, updated.Status)
		require.True(t, ja.refundedCoins.IsZero())
		require.True(t, ja.burnedCoins.IsZero())
		require.Empty(t, ja.fk.overturnedCalls)
		require.Empty(t, ja.fk.upheldCalls)
	})
}
