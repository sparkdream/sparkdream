package keeper_test

import (
	"testing"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// Test constants
const (
	TestDisplayName  = "TestUser"
	TestUsername     = "testuser"
	TestGuildName    = "Test Guild"
	TestGuildDesc    = "A test guild for unit testing"
	TestDisplayName2 = "AnotherUser"
	TestUsername2    = "anotheruser"
	TestGuildName2   = "Another Guild"
	TestReportReason = "Inappropriate name"
	TestAppealReason = "Name is not inappropriate"
)

// Test addresses
var (
	TestAddrCreator       = sdk.AccAddress([]byte("creator_________"))
	TestAddrMember1       = sdk.AccAddress([]byte("member1_________"))
	TestAddrMember2       = sdk.AccAddress([]byte("member2_________"))
	TestAddrMember3       = sdk.AccAddress([]byte("member3_________"))
	TestAddrReporter      = sdk.AccAddress([]byte("reporter________"))
	TestAddrTarget        = sdk.AccAddress([]byte("target__________"))
	TestAddrFounder       = sdk.AccAddress([]byte("founder_________"))
	TestAddrOfficer       = sdk.AccAddress([]byte("officer_________"))
	TestAddrCouncilPolicy = sdk.AccAddress([]byte("council_policy__"))
)

// alwaysMembers returns the bech32 strings of every test address that should
// be treated as a member by the default mockRepKeeper wired into initFixture.
// Tests that need a non-member can use a fresh address not in this set.
func alwaysMembers() map[string]struct{} {
	addrs := []sdk.AccAddress{
		TestAddrCreator, TestAddrMember1, TestAddrMember2, TestAddrMember3,
		TestAddrReporter, TestAddrTarget, TestAddrFounder, TestAddrOfficer,
		TestAddrCouncilPolicy,
	}
	out := make(map[string]struct{}, len(addrs))
	for _, a := range addrs {
		out[a.String()] = struct{}{}
	}
	return out
}

// SetupMemberProfile creates a member profile for testing
func SetupMemberProfile(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, displayName string, username string) {
	t.Helper()
	// Use the keeper's address codec to get the correct string representation
	addrStr, err := k.GetAddressCodec().BytesToString(addr)
	require.NoError(t, err, "failed to encode address")

	profile := types.MemberProfile{
		Address:     addrStr,
		DisplayName: displayName,
		Username:    username,
		SeasonXp:    0,
		SeasonLevel: 1,
		LifetimeXp:  0,
		GuildId:     0,
	}

	err = k.MemberProfile.Set(ctx, addrStr, profile)
	require.NoError(t, err, "failed to setup member profile")
}

// SetupMemberProfileWithGuild creates a member profile in a guild
func SetupMemberProfileWithGuild(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, guildID uint64) {
	t.Helper()
	addrStr, err := k.GetAddressCodec().BytesToString(addr)
	require.NoError(t, err, "failed to encode address")

	profile := types.MemberProfile{
		Address:     addrStr,
		DisplayName: "Member",
		Username:    "",
		SeasonXp:    0,
		SeasonLevel: 1,
		LifetimeXp:  0,
		GuildId:     guildID,
	}

	err = k.MemberProfile.Set(ctx, addrStr, profile)
	require.NoError(t, err, "failed to setup member profile")
}

// SetupBasicMemberProfile creates a basic member profile without guild
func SetupBasicMemberProfile(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress) {
	t.Helper()
	SetupMemberProfile(t, k, ctx, addr, "Display Name", "")
}

// GetAddrString is a helper to get consistent address encoding
func GetAddrString(t *testing.T, ac address.Codec, addr sdk.AccAddress) string {
	t.Helper()
	addrStr, err := ac.BytesToString(addr)
	require.NoError(t, err)
	return addrStr
}

// SetupGuild creates a guild for testing
func SetupGuild(t *testing.T, k keeper.Keeper, ctx sdk.Context, founder sdk.AccAddress, name string, description string) uint64 {
	t.Helper()

	founderStr, err := k.GetAddressCodec().BytesToString(founder)
	require.NoError(t, err, "failed to encode founder address")

	// Get next guild ID (add 1 because 0 means "no guild")
	seqVal, err := k.GuildSeq.Next(ctx)
	require.NoError(t, err, "failed to get guild ID")
	guildID := seqVal + 1

	guild := types.Guild{
		Id:             guildID,
		Name:           name,
		Description:    description,
		Founder:        founderStr,
		CreatedBlock:   ctx.BlockHeight(),
		InviteOnly:     false,
		Status:         types.GuildStatus_GUILD_STATUS_ACTIVE,
		Officers:       []string{},
		PendingInvites: []string{},
	}

	err = k.Guild.Set(ctx, guildID, guild)
	require.NoError(t, err, "failed to setup guild")

	return guildID
}

// SetupGuildWithMember creates a guild and adds a member
func SetupGuildWithMember(t *testing.T, k keeper.Keeper, ctx sdk.Context, founder sdk.AccAddress, member sdk.AccAddress) uint64 {
	t.Helper()

	memberStr, err := k.GetAddressCodec().BytesToString(member)
	require.NoError(t, err, "failed to encode member address")

	guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

	// Setup founder profile with guild
	SetupMemberProfileWithGuild(t, k, ctx, founder, guildID)

	// Setup member profile with guild
	SetupMemberProfileWithGuild(t, k, ctx, member, guildID)

	// Create membership record for member
	membership := types.GuildMembership{
		Member:                 memberStr,
		GuildId:                guildID,
		JoinedEpoch:            0,
		LeftEpoch:              0,
		GuildsJoinedThisSeason: 1,
	}
	err = k.GuildMembership.Set(ctx, memberStr, membership)
	require.NoError(t, err, "failed to setup guild membership")

	return guildID
}

// SetupDisplayNameModeration creates a moderation record
func SetupDisplayNameModeration(t *testing.T, k keeper.Keeper, ctx sdk.Context, member sdk.AccAddress, rejectedName string) {
	t.Helper()

	moderation := types.DisplayNameModeration{
		Member:       member.String(),
		RejectedName: rejectedName,
		Reason:       TestReportReason,
		ModeratedAt:  ctx.BlockHeight(),
		Active:       true,
	}

	err := k.DisplayNameModeration.Set(ctx, member.String(), moderation)
	require.NoError(t, err, "failed to setup display name moderation")
}

// AssertMemberProfileExists checks that a member profile exists
func AssertMemberProfileExists(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress) {
	t.Helper()

	_, err := k.MemberProfile.Get(ctx, addr.String())
	require.NoError(t, err, "member profile should exist")
}

// AssertMemberProfileGuildId checks member's guild ID
func AssertMemberProfileGuildId(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, expectedGuildId uint64) {
	t.Helper()

	profile, err := k.MemberProfile.Get(ctx, addr.String())
	require.NoError(t, err, "failed to get member profile")
	require.Equal(t, expectedGuildId, profile.GuildId, "guild ID mismatch")
}

// AssertGuildStatus checks a guild's status
func AssertGuildStatus(t *testing.T, k keeper.Keeper, ctx sdk.Context, guildID uint64, expectedStatus types.GuildStatus) {
	t.Helper()

	guild, err := k.Guild.Get(ctx, guildID)
	require.NoError(t, err, "failed to get guild")
	require.Equal(t, expectedStatus, guild.Status, "guild status mismatch")
}

// AssertDisplayNameModerationActive checks if moderation is active
func AssertDisplayNameModerationActive(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, expectedActive bool) {
	t.Helper()

	moderation, err := k.DisplayNameModeration.Get(ctx, addr.String())
	require.NoError(t, err, "failed to get moderation")
	require.Equal(t, expectedActive, moderation.Active, "moderation active state mismatch")
}

// AdvanceBlockHeight advances the block height
func AdvanceBlockHeight(ctx sdk.Context, blocks int64) sdk.Context {
	newHeight := ctx.BlockHeight() + blocks
	return ctx.WithBlockHeight(newHeight)
}

// AdvanceEpochs advances the block height by epochs
func AdvanceEpochs(ctx sdk.Context, params types.Params, epochs int64) sdk.Context {
	blocks := epochs * params.EpochBlocks
	return AdvanceBlockHeight(ctx, blocks)
}

// AddMemberToGuild adds an existing member to a guild with proper membership record
func AddMemberToGuild(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, guildID uint64) {
	t.Helper()

	addrStr, err := k.GetAddressCodec().BytesToString(addr)
	require.NoError(t, err, "failed to encode address")

	// Update member profile
	profile, err := k.MemberProfile.Get(ctx, addrStr)
	require.NoError(t, err, "member profile must exist")
	profile.GuildId = guildID
	err = k.MemberProfile.Set(ctx, addrStr, profile)
	require.NoError(t, err, "failed to update member profile")

	// Create membership record
	membership := types.GuildMembership{
		Member:                 addrStr,
		GuildId:                guildID,
		JoinedEpoch:            0,
		LeftEpoch:              0, // 0 means still in guild
		GuildsJoinedThisSeason: 1,
	}
	err = k.GuildMembership.Set(ctx, addrStr, membership)
	require.NoError(t, err, "failed to setup guild membership")
}

// SetupDefaultSeason creates a default active season for testing
func SetupDefaultSeason(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
	t.Helper()

	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100000,
		Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
	}
	err := k.Season.Set(ctx, season)
	require.NoError(t, err, "failed to setup default season")
}

// SetupGuildFounderMembership creates membership record for the guild founder
func SetupGuildFounderMembership(t *testing.T, k keeper.Keeper, ctx sdk.Context, founder sdk.AccAddress, guildID uint64) {
	t.Helper()

	founderStr, err := k.GetAddressCodec().BytesToString(founder)
	require.NoError(t, err, "failed to encode founder address")

	// Update founder profile with guild
	profile, err := k.MemberProfile.Get(ctx, founderStr)
	require.NoError(t, err, "founder profile must exist")
	profile.GuildId = guildID
	err = k.MemberProfile.Set(ctx, founderStr, profile)
	require.NoError(t, err, "failed to update founder profile")

	// Create membership record for founder
	membership := types.GuildMembership{
		Member:                 founderStr,
		GuildId:                guildID,
		JoinedEpoch:            0,
		LeftEpoch:              0,
		GuildsJoinedThisSeason: 1,
	}
	err = k.GuildMembership.Set(ctx, founderStr, membership)
	require.NoError(t, err, "failed to setup founder guild membership")
}
