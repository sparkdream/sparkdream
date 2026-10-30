package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Member-action appeals (REP-R-14 / FRM-02): an OVERTURNED appeal of a
// WARNING, DEMOTION or ZEROING undoes the action.

// seedReportedMember gives addr a member record with reputation, credits and
// DREAM, and a PENDING report against it.
func seedReportedMember(t *testing.T, f *fixture, addr sdk.AccAddress) {
	t.Helper()
	seedMigrant(t, f, addr, 5_000_000)
	m := mustMember(t, f, addr)
	m.ReputationScores = map[string]string{"phoenix": "100.000000000000000000", "aurora": "40.000000000000000000"}
	m.InvitationCredits = 2
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), m))
	require.NoError(t, f.keeper.MemberReport.Set(f.ctx, addr.String(), types.MemberReport{
		Member:            addr.String(),
		Reason:            "spam",
		RecommendedAction: types.GovActionType_GOV_ACTION_TYPE_WARNING,
		Status:            types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		CreatedAt:         f.ctx.BlockTime().Unix(),
	}))
}

func resolveReport(t *testing.T, f *fixture, addr sdk.AccAddress, action types.GovActionType) {
	t.Helper()
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
		Creator: f.keeper.GetAuthorityString(),
		Member:  addr.String(),
		Action:  uint64(action),
		Reason:  "resolved",
	})
	require.NoError(t, err)
}

// appealAndDecide files the member's appeal (signed by signer, naming target)
// and resolves it with verdict. Returns the appeal id.
func appealAndDecide(t *testing.T, f *fixture, signer, target sdk.AccAddress, action types.GovActionType, verdict types.GovAppealStatus) uint64 {
	t.Helper()
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
		Creator:      signer.String(),
		ActionType:   uint64(action),
		ActionTarget: target.String(),
		AppealReason: "unjustified",
	})
	require.NoError(t, err)
	rest, err := f.keeper.MemberActionRestorations.Get(f.ctx, f.keeper.Canonical(f.ctx, target.String()))
	require.NoError(t, err)
	require.True(t, rest.Appealed, "the record carries the appeal")
	_, err = ms.ResolveGovActionAppeal(f.ctx, &types.MsgResolveGovActionAppeal{
		Resolver: f.keeper.GetAuthorityString(),
		AppealId: rest.AppealId,
		Verdict:  verdict,
		Reason:   "decided",
	})
	require.NoError(t, err)
	has, err := f.keeper.MemberActionRestorations.Has(f.ctx, rest.Member)
	require.NoError(t, err)
	require.False(t, has, "the record is dropped at the terminal outcome")
	return rest.AppealId
}

func requireRep(t *testing.T, m types.Member, tag, want string) {
	t.Helper()
	got, err := math.LegacyNewDecFromStr(m.ReputationScores[tag])
	require.NoError(t, err)
	require.True(t, got.Equal(math.LegacyMustNewDecFromStr(want)), "%s: got %s want %s", tag, got, want)
}

func TestMemberActionAppeal_WarningOverturnedDeletesWarning(t *testing.T) {
	f := migrationFixture(t)
	addr := sdk.AccAddress([]byte("phoenix-warned_____"))
	seedReportedMember(t, f, addr)
	resolveReport(t, f, addr, types.GovActionType_GOV_ACTION_TYPE_WARNING)

	rest, err := f.keeper.MemberActionRestorations.Get(f.ctx, addr.String())
	require.NoError(t, err)
	_, err = f.keeper.MemberWarning.Get(f.ctx, rest.WarningId)
	require.NoError(t, err, "the resolution created the warning")

	appealAndDecide(t, f, addr, addr, types.GovActionType_GOV_ACTION_TYPE_WARNING, types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED)
	_, err = f.keeper.MemberWarning.Get(f.ctx, rest.WarningId)
	require.Error(t, err, "an overturned warning is deleted")
}

func TestMemberActionAppeal_DemotionOverturnedRestoresReputation(t *testing.T) {
	f := migrationFixture(t)
	addr := sdk.AccAddress([]byte("aurora-demoted_____"))
	seedReportedMember(t, f, addr)
	resolveReport(t, f, addr, types.GovActionType_GOV_ACTION_TYPE_DEMOTION)
	requireRep(t, mustMember(t, f, addr), "phoenix", "70") // 30% severe slash

	appealAndDecide(t, f, addr, addr, types.GovActionType_GOV_ACTION_TYPE_DEMOTION, types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED)
	m := mustMember(t, f, addr)
	requireRep(t, m, "phoenix", "100")
	requireRep(t, m, "aurora", "40")
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, m.TrustLevel)
}

func TestMemberActionAppeal_DemotionUpheldKeepsSlash(t *testing.T) {
	f := migrationFixture(t)
	addr := sdk.AccAddress([]byte("zenith-demoted_____"))
	seedReportedMember(t, f, addr)
	resolveReport(t, f, addr, types.GovActionType_GOV_ACTION_TYPE_DEMOTION)
	appealAndDecide(t, f, addr, addr, types.GovActionType_GOV_ACTION_TYPE_DEMOTION, types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD)
	requireRep(t, mustMember(t, f, addr), "phoenix", "70")
}

func TestMemberActionAppeal_ZeroingOverturnedRestoresMember(t *testing.T) {
	f := migrationFixture(t)
	addr := sdk.AccAddress([]byte("phoenix-zeroed_____"))
	seedReportedMember(t, f, addr)
	before := mustMember(t, f, addr)
	resolveReport(t, f, addr, types.GovActionType_GOV_ACTION_TYPE_ZEROING)

	zeroed := mustMember(t, f, addr)
	require.Equal(t, types.MemberStatus_MEMBER_STATUS_ZEROED, zeroed.Status)
	require.True(t, zeroed.DreamBalance.IsZero())
	rest, err := f.keeper.MemberActionRestorations.Get(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, before.DreamBalance.String(), rest.BurnedDream)

	appealAndDecide(t, f, addr, addr, types.GovActionType_GOV_ACTION_TYPE_ZEROING, types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED)
	m := mustMember(t, f, addr)
	require.Equal(t, types.MemberStatus_MEMBER_STATUS_ACTIVE, m.Status)
	require.Equal(t, before.TrustLevel, m.TrustLevel)
	require.Equal(t, before.InvitationCredits, m.InvitationCredits)
	require.Equal(t, before.DreamBalance.String(), m.DreamBalance.String(), "the burned liquid DREAM is re-minted")
	require.True(t, m.LifetimeBurned.IsZero())
	require.Zero(t, m.ZeroedCount)
	requireRep(t, m, "phoenix", "100")
	requireRep(t, m, "aurora", "40")
	lifetime, err := math.LegacyNewDecFromStr(m.LifetimeReputation["phoenix"])
	require.NoError(t, err)
	require.True(t, lifetime.IsZero(), "the zeroing's archive entry is taken back")
}

func TestMemberActionAppeal_ZeroingUpheldStays(t *testing.T) {
	f := migrationFixture(t)
	addr := sdk.AccAddress([]byte("aurora-zeroed______"))
	seedReportedMember(t, f, addr)
	resolveReport(t, f, addr, types.GovActionType_GOV_ACTION_TYPE_ZEROING)
	appealAndDecide(t, f, addr, addr, types.GovActionType_GOV_ACTION_TYPE_ZEROING, types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD)
	m := mustMember(t, f, addr)
	require.Equal(t, types.MemberStatus_MEMBER_STATUS_ZEROED, m.Status)
	require.True(t, m.DreamBalance.IsZero())
}

// A member zeroed for another reason while a DEMOTION appeal is pending is
// not resurrected by the overturn.
func TestMemberActionAppeal_DemotionOverturnDoesNotResurrect(t *testing.T) {
	f := migrationFixture(t)
	addr := sdk.AccAddress([]byte("zenith-twice_______"))
	seedReportedMember(t, f, addr)
	resolveReport(t, f, addr, types.GovActionType_GOV_ACTION_TYPE_DEMOTION)

	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
		Creator: addr.String(), ActionType: uint64(types.GovActionType_GOV_ACTION_TYPE_DEMOTION),
		ActionTarget: addr.String(), AppealReason: "unjustified",
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.ZeroMember(f.ctx, addr, "unrelated"))
	rest, err := f.keeper.MemberActionRestorations.Get(f.ctx, addr.String())
	require.NoError(t, err)
	_, err = ms.ResolveGovActionAppeal(f.ctx, &types.MsgResolveGovActionAppeal{
		Resolver: f.keeper.GetAuthorityString(), AppealId: rest.AppealId,
		Verdict: types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED, Reason: "decided",
	})
	require.NoError(t, err)
	m := mustMember(t, f, addr)
	require.Equal(t, types.MemberStatus_MEMBER_STATUS_ZEROED, m.Status)
	requireRep(t, m, "phoenix", "0")
}

// The member migrates between the resolution and the verdict: the appeal is
// filed by the new key against the old address and the reversal lands on the
// new identity.
func TestMemberActionAppeal_FollowsMigration(t *testing.T) {
	f := migrationFixture(t)
	oldAddr := sdk.AccAddress([]byte("phoenix-oldkey_____"))
	newAddr := sdk.AccAddress([]byte("phoenix-newkey_____"))
	seedReportedMember(t, f, oldAddr)
	resolveReport(t, f, oldAddr, types.GovActionType_GOV_ACTION_TYPE_DEMOTION)

	finalizeKeyMigration(t, f, oldAddr, newAddr)
	has, err := f.keeper.MemberActionRestorations.Has(f.ctx, newAddr.String())
	require.NoError(t, err)
	require.True(t, has, "the record is re-keyed with the identity")

	appealAndDecide(t, f, newAddr, oldAddr, types.GovActionType_GOV_ACTION_TYPE_DEMOTION, types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED)
	requireRep(t, mustMember(t, f, newAddr), "phoenix", "100")
}

// With no appeal, the record is dropped once the filing window closes, and a
// late appeal is refused.
func TestMemberActionAppeal_RecordPrunedAfterWindow(t *testing.T) {
	f := migrationFixture(t)
	addr := sdk.AccAddress([]byte("aurora-quiet_______"))
	seedReportedMember(t, f, addr)
	resolveReport(t, f, addr, types.GovActionType_GOV_ACTION_TYPE_ZEROING)

	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gen.MemberActionRestorationList, 1, "exported while appealable")

	advance(f, types.DefaultAppealDeadline, 1)
	f.keeper.PruneMemberActionRestorations(f.ctx)
	require.True(t, f.keeper.HasMemberActionRestoration(f.ctx, addr.String()), "still open at the window's last second")

	advance(f, 1, 1)
	f.keeper.PruneMemberActionRestorations(f.ctx)
	require.False(t, f.keeper.HasMemberActionRestoration(f.ctx, addr.String()))
}

// A pending appeal holds the record past the filing window until its outcome;
// a TIMEOUT drops it without reversing anything.
func TestMemberActionAppeal_TimeoutDropsRecord(t *testing.T) {
	f := migrationFixture(t)
	addr := sdk.AccAddress([]byte("zenith-timeout_____"))
	seedReportedMember(t, f, addr)
	resolveReport(t, f, addr, types.GovActionType_GOV_ACTION_TYPE_DEMOTION)
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
		Creator: addr.String(), ActionType: uint64(types.GovActionType_GOV_ACTION_TYPE_DEMOTION),
		ActionTarget: addr.String(), AppealReason: "unjustified",
	})
	require.NoError(t, err)

	advance(f, types.DefaultAppealDeadline+1, 1)
	f.keeper.PruneMemberActionRestorations(f.ctx)
	require.True(t, f.keeper.HasMemberActionRestoration(f.ctx, addr.String()), "kept while the appeal is pending")

	require.NoError(t, f.keeper.TimeoutExpiredAppeals(f.ctx))
	require.False(t, f.keeper.HasMemberActionRestoration(f.ctx, addr.String()))
	requireRep(t, mustMember(t, f, addr), "phoenix", "70")
}
