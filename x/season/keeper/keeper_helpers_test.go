package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/types"
)

func TestGetCurrentEpoch(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("returns epoch based on block height", func(t *testing.T) {
		params, err := k.Params.Get(ctx)
		require.NoError(t, err)

		// Block 0 should be epoch 0
		ctx = ctx.WithBlockHeight(0)
		epoch := k.GetCurrentEpoch(ctx)
		require.Equal(t, int64(0), epoch)

		// Block equal to epoch blocks should be epoch 1
		ctx = ctx.WithBlockHeight(params.EpochBlocks)
		epoch = k.GetCurrentEpoch(ctx)
		require.Equal(t, int64(1), epoch)

		// Block 2x epoch blocks should be epoch 2
		ctx = ctx.WithBlockHeight(params.EpochBlocks * 2)
		epoch = k.GetCurrentEpoch(ctx)
		require.Equal(t, int64(2), epoch)
	})
}

func TestBlockToEpoch(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("converts block to epoch correctly", func(t *testing.T) {
		params, err := k.Params.Get(ctx)
		require.NoError(t, err)

		require.Equal(t, int64(0), k.BlockToEpoch(ctx, 0))
		require.Equal(t, int64(1), k.BlockToEpoch(ctx, params.EpochBlocks))
		require.Equal(t, int64(2), k.BlockToEpoch(ctx, params.EpochBlocks*2))
		require.Equal(t, int64(0), k.BlockToEpoch(ctx, params.EpochBlocks-1))
	})
}

func TestEpochToBlock(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("converts epoch to block correctly", func(t *testing.T) {
		params, err := k.Params.Get(ctx)
		require.NoError(t, err)

		require.Equal(t, int64(0), k.EpochToBlock(ctx, 0))
		require.Equal(t, params.EpochBlocks, k.EpochToBlock(ctx, 1))
		require.Equal(t, params.EpochBlocks*2, k.EpochToBlock(ctx, 2))
	})
}

func TestIsGovAuthority(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("returns true for governance authority", func(t *testing.T) {
		authorityAddr, err := f.addressCodec.BytesToString(k.GetAuthority())
		require.NoError(t, err)
		require.True(t, k.IsGovAuthority(ctx, authorityAddr))
	})

	t.Run("returns false for non-authority", func(t *testing.T) {
		nonAuthority := sdk.AccAddress([]byte("non_authority____"))
		nonAuthorityStr, err := f.addressCodec.BytesToString(nonAuthority)
		require.NoError(t, err)
		require.False(t, k.IsGovAuthority(ctx, nonAuthorityStr))
	})

	t.Run("returns false for invalid address", func(t *testing.T) {
		require.False(t, k.IsGovAuthority(ctx, "invalid-address"))
	})
}

func TestIsOperationsCommittee(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	// Without commons keeper, falls back to gov authority check
	t.Run("fallback to gov authority without commons keeper", func(t *testing.T) {
		authorityAddr, err := f.addressCodec.BytesToString(k.GetAuthority())
		require.NoError(t, err)
		require.True(t, k.IsOperationsCommittee(ctx, authorityAddr))

		nonAuthority := sdk.AccAddress([]byte("non_authority____"))
		nonAuthorityStr, err := f.addressCodec.BytesToString(nonAuthority)
		require.NoError(t, err)
		require.False(t, k.IsOperationsCommittee(ctx, nonAuthorityStr))
	})
}

func TestIsCommonsCouncil(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	// Without commons keeper, falls back to gov authority check
	t.Run("fallback to gov authority without commons keeper", func(t *testing.T) {
		authorityAddr, err := f.addressCodec.BytesToString(k.GetAuthority())
		require.NoError(t, err)
		require.True(t, k.IsCommonsCouncil(ctx, authorityAddr))

		nonAuthority := sdk.AccAddress([]byte("non_authority____"))
		nonAuthorityStr, err := f.addressCodec.BytesToString(nonAuthority)
		require.NoError(t, err)
		require.False(t, k.IsCommonsCouncil(ctx, nonAuthorityStr))
	})
}

func TestIsMember(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("returns false for non-existent profile", func(t *testing.T) {
		addr := sdk.AccAddress([]byte("nonexistent_____"))
		addrStr, err := f.addressCodec.BytesToString(addr)
		require.NoError(t, err)
		require.False(t, k.IsMember(ctx, addrStr))
	})

	t.Run("returns true for existing profile", func(t *testing.T) {
		addr := TestAddrCreator
		addrStr, err := f.addressCodec.BytesToString(addr)
		require.NoError(t, err)

		SetupBasicMemberProfile(t, k, ctx, addr)
		require.True(t, k.IsMember(ctx, addrStr))
	})
}

func TestHasMemberProfile(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("returns false for non-existent profile", func(t *testing.T) {
		addr := sdk.AccAddress([]byte("nonexistent_____"))
		addrStr, err := f.addressCodec.BytesToString(addr)
		require.NoError(t, err)
		require.False(t, k.HasMemberProfile(ctx, addrStr))
	})

	t.Run("returns true for existing profile", func(t *testing.T) {
		addr := TestAddrMember1
		addrStr, err := f.addressCodec.BytesToString(addr)
		require.NoError(t, err)

		SetupBasicMemberProfile(t, k, ctx, addr)
		require.True(t, k.HasMemberProfile(ctx, addrStr))
	})
}

func TestValidateDisplayName(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("valid display name", func(t *testing.T) {
		err := k.ValidateDisplayName(ctx, "ValidName")
		require.NoError(t, err)
	})

	t.Run("display name too short", func(t *testing.T) {
		err := k.ValidateDisplayName(ctx, "") // Empty string is below min length of 1
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameTooShort)
	})

	t.Run("display name too long", func(t *testing.T) {
		longName := ""
		for i := 0; i < 100; i++ {
			longName += "a"
		}
		err := k.ValidateDisplayName(ctx, longName)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameTooLong)
	})
}

func TestValidateUsername(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("valid username", func(t *testing.T) {
		err := k.ValidateUsername(ctx, "validuser123")
		require.NoError(t, err)
	})

	t.Run("valid username with underscores", func(t *testing.T) {
		err := k.ValidateUsername(ctx, "valid_user_123")
		require.NoError(t, err)
	})

	t.Run("username too short", func(t *testing.T) {
		err := k.ValidateUsername(ctx, "ab")
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUsernameTooShort)
	})

	t.Run("username with invalid characters", func(t *testing.T) {
		err := k.ValidateUsername(ctx, "Invalid-User!")
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUsernameInvalidChars)
	})

	t.Run("username with uppercase", func(t *testing.T) {
		err := k.ValidateUsername(ctx, "InvalidUser")
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUsernameInvalidChars)
	})
}

func TestValidateGuildName(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("valid guild name", func(t *testing.T) {
		err := k.ValidateGuildName(ctx, "My Awesome Guild")
		require.NoError(t, err)
	})

	t.Run("guild name too short", func(t *testing.T) {
		err := k.ValidateGuildName(ctx, "ab")
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNameTooShort)
	})

	t.Run("guild name taken", func(t *testing.T) {
		// Create a guild first
		SetupGuild(t, k, ctx, TestAddrFounder, "Existing Guild", "Description")

		// Try to validate the same name
		err := k.ValidateGuildName(ctx, "Existing Guild")
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNameTaken)
	})

	t.Run("guild name case insensitive check", func(t *testing.T) {
		// The guild "Existing Guild" was created above
		err := k.ValidateGuildName(ctx, "existing guild")
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNameTaken)
	})
}

func TestValidateGuildDescription(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("valid description", func(t *testing.T) {
		err := k.ValidateGuildDescription(ctx, "This is a valid guild description.")
		require.NoError(t, err)
	})

	t.Run("empty description is valid", func(t *testing.T) {
		err := k.ValidateGuildDescription(ctx, "")
		require.NoError(t, err)
	})
}

func TestGuildMembershipHelpers(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	founder := TestAddrFounder
	officer := TestAddrOfficer
	member := TestAddrMember1

	// Setup profiles
	SetupBasicMemberProfile(t, k, ctx, founder)
	SetupBasicMemberProfile(t, k, ctx, officer)
	SetupBasicMemberProfile(t, k, ctx, member)

	// Create guild
	guildID := SetupGuild(t, k, ctx, founder, "Test Guild", "Description")

	// Add officer
	guild, _ := k.Guild.Get(ctx, guildID)
	guild.Officers = []string{officer.String()}
	k.Guild.Set(ctx, guildID, guild)

	// Create membership for member
	membership := types.GuildMembership{
		Member:                 member.String(),
		GuildId:                guildID,
		JoinedEpoch:            0,
		LeftEpoch:              0,
		GuildsJoinedThisSeason: 1,
	}
	k.GuildMembership.Set(ctx, member.String(), membership)

	t.Run("IsGuildFounder", func(t *testing.T) {
		require.True(t, k.IsGuildFounder(ctx, guildID, founder.String()))
		require.False(t, k.IsGuildFounder(ctx, guildID, member.String()))
		require.False(t, k.IsGuildFounder(ctx, guildID, officer.String()))
	})

	t.Run("IsGuildOfficer", func(t *testing.T) {
		require.True(t, k.IsGuildOfficer(ctx, guildID, officer.String()))
		require.False(t, k.IsGuildOfficer(ctx, guildID, founder.String()))
		require.False(t, k.IsGuildOfficer(ctx, guildID, member.String()))
	})

	t.Run("IsGuildFounderOrOfficer", func(t *testing.T) {
		require.True(t, k.IsGuildFounderOrOfficer(ctx, guildID, founder.String()))
		require.True(t, k.IsGuildFounderOrOfficer(ctx, guildID, officer.String()))
		require.False(t, k.IsGuildFounderOrOfficer(ctx, guildID, member.String()))
	})

	t.Run("IsGuildMember", func(t *testing.T) {
		require.True(t, k.IsGuildMember(ctx, guildID, member.String()))
		// Members who left should not be considered members
		membership.LeftEpoch = 1
		k.GuildMembership.Set(ctx, member.String(), membership)
		require.False(t, k.IsGuildMember(ctx, guildID, member.String()))
	})
}

func TestCalculateLevel(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	t.Run("level 1 for zero XP", func(t *testing.T) {
		level := k.CalculateLevel(ctx, 0)
		require.Equal(t, uint64(1), level)
	})

	t.Run("level increases with XP", func(t *testing.T) {
		params, _ := k.Params.Get(ctx)
		if len(params.LevelThresholds) > 0 {
			// XP at first threshold should be level 1
			level := k.CalculateLevel(ctx, params.LevelThresholds[0])
			require.GreaterOrEqual(t, level, uint64(1))
		}
	})
}

func TestHasQuestPrerequisite(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	member := TestAddrMember1
	addrStr := member.String()

	t.Run("no prerequisite returns true", func(t *testing.T) {
		require.True(t, k.HasQuestPrerequisite(ctx, addrStr, ""))
	})

	t.Run("unmet prerequisite returns false", func(t *testing.T) {
		require.False(t, k.HasQuestPrerequisite(ctx, addrStr, "quest_1"))
	})

	t.Run("met prerequisite returns true", func(t *testing.T) {
		// Create completed progress
		progress := types.MemberQuestProgress{
			MemberQuest:    addrStr + ":quest_1",
			Completed:      true,
			CompletedBlock: 100,
		}
		k.MemberQuestProgress.Set(ctx, addrStr+":quest_1", progress)

		require.True(t, k.HasQuestPrerequisite(ctx, addrStr, "quest_1"))
	})
}

func TestHasUnlockedTitle(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	member := TestAddrMember1
	addrStr := member.String()

	// Setup profile with unlocked title
	profile := types.MemberProfile{
		Address:        addrStr,
		UnlockedTitles: []string{"title_1", "title_2"},
		ArchivedTitles: []string{"archived_title"},
	}
	k.MemberProfile.Set(ctx, addrStr, profile)

	t.Run("returns true for unlocked title", func(t *testing.T) {
		require.True(t, k.HasUnlockedTitle(ctx, addrStr, "title_1"))
		require.True(t, k.HasUnlockedTitle(ctx, addrStr, "title_2"))
	})

	t.Run("returns true for archived title", func(t *testing.T) {
		require.True(t, k.HasUnlockedTitle(ctx, addrStr, "archived_title"))
	})

	t.Run("returns false for non-unlocked title", func(t *testing.T) {
		require.False(t, k.HasUnlockedTitle(ctx, addrStr, "title_99"))
	})
}

func TestCrossModuleHelpers(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper

	addr := TestAddrMember1
	addrStr := addr.String()

	// Without rep/name keepers, these should gracefully skip
	t.Run("BurnDREAM without rep keeper", func(t *testing.T) {
		err := k.BurnDREAM(ctx, addrStr, 100)
		require.NoError(t, err, "should not error when rep keeper is nil")
	})

	t.Run("LockDREAM without rep keeper", func(t *testing.T) {
		err := k.LockDREAM(ctx, addrStr, 100)
		require.NoError(t, err, "should not error when rep keeper is nil")
	})

	t.Run("UnlockDREAM without rep keeper", func(t *testing.T) {
		err := k.UnlockDREAM(ctx, addrStr, 100)
		require.NoError(t, err, "should not error when rep keeper is nil")
	})

	t.Run("GetDREAMBalance without rep keeper", func(t *testing.T) {
		balance, err := k.GetDREAMBalance(ctx, addrStr)
		require.NoError(t, err, "should not error when rep keeper is nil")
		require.Equal(t, uint64(0), balance)
	})

	t.Run("ReserveName without name keeper", func(t *testing.T) {
		err := k.ReserveName(ctx, "testname", "guild", addrStr)
		require.NoError(t, err, "should not error when name keeper is nil")
	})

	t.Run("ReleaseName without name keeper", func(t *testing.T) {
		err := k.ReleaseName(ctx, "testname", addrStr)
		require.NoError(t, err, "should not error when name keeper is nil")
	})

	t.Run("IsNameAvailable without name keeper", func(t *testing.T) {
		available := k.IsNameAvailable(ctx, "testname")
		require.True(t, available, "should return true when name keeper is nil")
	})
}
