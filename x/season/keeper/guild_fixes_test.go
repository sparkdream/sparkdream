package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// guildFixture sets up a founder's invite-only guild created through
// MsgCreateGuild and the profiles of two more members.
func guildFixture(t *testing.T, inviteOnly bool) (sdk.Context, keeper.Keeper, types.MsgServer, string, string, string) {
	t.Helper()
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	founder, _ := f.addressCodec.BytesToString(TestAddrFounder)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	m2, _ := f.addressCodec.BytesToString(TestAddrMember2)
	for _, a := range []sdk.AccAddress{TestAddrFounder, TestAddrMember1, TestAddrMember2} {
		SetupBasicMemberProfile(t, k, ctx, a)
	}
	_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{Creator: founder, Name: "phoenix-guild", Description: "d", InviteOnly: inviteOnly})
	require.NoError(t, err)
	return ctx, k, ms, founder, m1, m2
}

// An invite record whose invitee is missing from the guild's pending list
// must not panic the handlers that rebuild that list (the capacity was
// len-1, which is -1 on an empty list).
func TestGuildInviteHandlersTolerateEmptyPendingList(t *testing.T) {
	setOrphanInvite := func(t *testing.T, ctx sdk.Context, k keeper.Keeper, founder, invitee string) {
		t.Helper()
		require.NoError(t, k.GuildInvite.Set(ctx, fmt.Sprintf("1:%s", invitee), types.GuildInvite{
			GuildInvitee: invitee, Inviter: founder, GuildId: 1,
		}))
		g, err := k.Guild.Get(ctx, 1)
		require.NoError(t, err)
		require.Empty(t, g.PendingInvites)
	}

	t.Run("accept", func(t *testing.T) {
		ctx, k, ms, founder, m1, _ := guildFixture(t, true)
		setOrphanInvite(t, ctx, k, founder, m1)
		require.NotPanics(t, func() {
			_, err := ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{Creator: m1, GuildId: 1})
			require.NoError(t, err)
		})
	})
	t.Run("join", func(t *testing.T) {
		ctx, k, ms, founder, m1, _ := guildFixture(t, true)
		setOrphanInvite(t, ctx, k, founder, m1)
		require.NotPanics(t, func() {
			_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{Creator: m1, GuildId: 1})
			require.NoError(t, err)
		})
	})
	t.Run("revoke", func(t *testing.T) {
		ctx, k, ms, founder, m1, _ := guildFixture(t, true)
		setOrphanInvite(t, ctx, k, founder, m1)
		require.NotPanics(t, func() {
			_, err := ms.RevokeGuildInvite(ctx, &types.MsgRevokeGuildInvite{Creator: founder, GuildId: 1, Invitee: m1})
			require.NoError(t, err)
		})
	})
}

// Leaving a guild in epoch 0 records LeftEpoch 0; the hop cooldown must
// still apply.
func TestGuildHopCooldownAppliesToEpochZeroLeave(t *testing.T) {
	ctx, k, ms, founder, m1, _ := guildFixture(t, false)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	require.Positive(t, params.GuildHopCooldownEpochs)
	require.Zero(t, k.GetCurrentEpoch(ctx))
	params.MinGuildMembers = 1 // the guild stays active when the member leaves
	require.NoError(t, k.Params.Set(ctx, params))

	_, err = ms.JoinGuild(ctx, &types.MsgJoinGuild{Creator: m1, GuildId: 1})
	require.NoError(t, err)
	_, err = ms.LeaveGuild(ctx, &types.MsgLeaveGuild{Creator: m1})
	require.NoError(t, err)

	_, err = ms.JoinGuild(ctx, &types.MsgJoinGuild{Creator: m1, GuildId: 1})
	require.ErrorIs(t, err, types.ErrGuildHopCooldown, "rejoin")
	_, err = ms.CreateGuild(ctx, &types.MsgCreateGuild{Creator: m1, Name: "aurora-guild", Description: "d"})
	require.ErrorIs(t, err, types.ErrGuildHopCooldown, "create")

	_, err = ms.InviteToGuild(ctx, &types.MsgInviteToGuild{Creator: founder, GuildId: 1, Invitee: m1})
	require.NoError(t, err)
	_, err = ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{Creator: m1, GuildId: 1})
	require.ErrorIs(t, err, types.ErrGuildHopCooldown, "accept")

	// After the cooldown the member may join again.
	ctx = AdvanceEpochs(ctx, params, int64(params.GuildHopCooldownEpochs))
	_, err = ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{Creator: m1, GuildId: 1})
	if err != nil {
		// The invite may have expired over the cooldown; joining is open.
		require.ErrorIs(t, err, types.ErrGuildInviteExpired)
		_, err = ms.JoinGuild(ctx, &types.MsgJoinGuild{Creator: m1, GuildId: 1})
	}
	require.NoError(t, err)
}

// JoinGuild refuses an expired invite to an invite-only guild, as
// AcceptGuildInvite does.
func TestJoinGuildRefusesExpiredInvite(t *testing.T) {
	ctx, k, ms, founder, m1, _ := guildFixture(t, true)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	params.GuildInviteTtlEpochs = 2
	require.NoError(t, k.Params.Set(ctx, params))

	_, err = ms.InviteToGuild(ctx, &types.MsgInviteToGuild{Creator: founder, GuildId: 1, Invitee: m1})
	require.NoError(t, err)

	expired := AdvanceEpochs(ctx, params, 3)
	_, err = ms.JoinGuild(expired, &types.MsgJoinGuild{Creator: m1, GuildId: 1})
	require.ErrorIs(t, err, types.ErrGuildInviteExpired)

	// Before expiry the invite works.
	_, err = ms.JoinGuild(AdvanceEpochs(ctx, params, 2), &types.MsgJoinGuild{Creator: m1, GuildId: 1})
	require.NoError(t, err)
}

// The current founder of a guild frozen below its minimum size has nothing
// to claim; a remaining member still may.
func TestClaimGuildFounderRefusesCurrentFounder(t *testing.T) {
	ctx, k, ms, founder, m1, _ := guildFixture(t, false)
	_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{Creator: m1, GuildId: 1})
	require.NoError(t, err)
	g, err := k.Guild.Get(ctx, 1)
	require.NoError(t, err)
	g.Status = types.GuildStatus_GUILD_STATUS_FROZEN
	require.NoError(t, k.Guild.Set(ctx, 1, g))

	_, err = ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{Creator: founder, GuildId: 1})
	require.ErrorIs(t, err, types.ErrAlreadyGuildFounder)

	_, err = ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{Creator: m1, GuildId: 1})
	require.NoError(t, err)
	g, err = k.Guild.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, m1, g.Founder)
}

// AbandonQuest touches only an existing profile; it must not create an
// empty, address-less one.
func TestAbandonQuestDoesNotCreateProfile(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	creator, _ := f.addressCodec.BytesToString(TestAddrCreator)

	SetupQuest(t, k, ctx, "quest1", 50, true)
	key := fmt.Sprintf("%s:%s", creator, "quest1")
	require.NoError(t, k.MemberQuestProgress.Set(ctx, key, types.MemberQuestProgress{MemberQuest: key}))

	_, err := ms.AbandonQuest(ctx, &types.MsgAbandonQuest{Creator: creator, QuestId: "quest1"})
	require.NoError(t, err)
	_, err = k.MemberProfile.Get(ctx, creator)
	require.Error(t, err, "no profile is created")
}
