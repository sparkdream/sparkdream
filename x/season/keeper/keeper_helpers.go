package keeper

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"
)

// Epoch calculations

// GetCurrentEpoch returns the current epoch number based on block height
func (k Keeper) GetCurrentEpoch(ctx context.Context) int64 {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0
	}
	if params.EpochBlocks <= 0 {
		return 0
	}
	return sdkCtx.BlockHeight() / params.EpochBlocks
}

// BlockToEpoch converts a block height to an epoch number
func (k Keeper) BlockToEpoch(ctx context.Context, block int64) int64 {
	params, err := k.Params.Get(ctx)
	if err != nil || params.EpochBlocks <= 0 {
		return 0
	}
	return block / params.EpochBlocks
}

// EpochToBlock converts an epoch number to its starting block height
func (k Keeper) EpochToBlock(ctx context.Context, epoch int64) int64 {
	params, err := k.Params.Get(ctx)
	if err != nil || params.EpochBlocks <= 0 {
		return 0
	}
	return epoch * params.EpochBlocks
}

// GetEpochDuration returns the epoch duration in seconds.
// Assumes ~5 second block times (Cosmos SDK default).
// Used by x/blog, x/forum, x/collect for anonymous action nullifier scoping.
func (k Keeper) GetEpochDuration(ctx context.Context) int64 {
	const blockTimeSeconds int64 = 5
	params, err := k.Params.Get(ctx)
	if err != nil || params.EpochBlocks <= 0 {
		return 86400 // fallback: 1 day
	}
	return params.EpochBlocks * blockTimeSeconds
}

// Authority checks (cross-module stubs)

// IsGovAuthority checks if the address is the governance authority
func (k Keeper) IsGovAuthority(ctx context.Context, addr string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return bytes.Equal(k.authority, addrBytes)
}

// IsOperationsCommittee checks if the address is a member of the Operations Committee
func (k Keeper) IsOperationsCommittee(ctx context.Context, addr string) bool {
	// If commons keeper is available, use it
	if k.commonsKeeper != nil {
		addrBytes, err := k.addressCodec.StringToBytes(addr)
		if err != nil {
			return false
		}
		isMember, err := k.commonsKeeper.IsCommitteeMember(ctx, addrBytes, "commons", "operations")
		if err == nil {
			return isMember
		}
	}
	// Fallback: treat governance authority as operations committee
	return k.IsGovAuthority(ctx, addr)
}

// IsCouncilAuthorized checks if the address is authorized via governance authority,
// council policy address, or committee membership.
// Delegates to x/commons IsCouncilAuthorized when available.
// Falls back to IsGovAuthority when x/commons is not wired.
func (k Keeper) isCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if k.commonsKeeper == nil {
		return k.IsGovAuthority(ctx, addr)
	}
	return k.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// IsAuthorizedForGamification checks if the address is authorized to manage
// achievements, titles, and quests. Delegates to x/commons IsCouncilAuthorized.
// Authorization is granted to:
// 1. Governance module authority (x/gov)
// 2. Commons Council policy address (council proposals)
// 3. Commons Operations Committee members (direct action)
func (k Keeper) IsAuthorizedForGamification(ctx context.Context, addr string) bool {
	if k.IsGovAuthority(ctx, addr) {
		return true
	}
	if k.commonsKeeper == nil {
		return false
	}
	return k.commonsKeeper.IsCouncilAuthorized(ctx, addr, "commons", "operations")
}

// IsCommonsCouncil checks if the address is a member of the Commons Council
func (k Keeper) IsCommonsCouncil(ctx context.Context, addr string) bool {
	// If commons keeper is available, use it
	if k.commonsKeeper != nil {
		addrBytes, err := k.addressCodec.StringToBytes(addr)
		if err != nil {
			return false
		}
		// Check if member of any committee in commons council
		for _, committee := range []string{"operations", "hr"} {
			isMember, err := k.commonsKeeper.IsCommitteeMember(ctx, addrBytes, "commons", committee)
			if err == nil && isMember {
				return true
			}
		}
	}
	// Fallback: treat governance authority as commons council
	return k.IsGovAuthority(ctx, addr)
}

// IsMember checks if the address is a registered member (via x/rep)
func (k Keeper) IsMember(ctx context.Context, addr string) bool {
	// If rep keeper is available, use it
	if k.repKeeper != nil {
		accAddr, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return false
		}
		return k.repKeeper.IsMember(ctx, accAddr)
	}
	// Fallback: check if they have a profile in x/season
	_, err := k.MemberProfile.Get(ctx, addr)
	return err == nil
}

// HasMemberProfile checks if a member has a profile
func (k Keeper) HasMemberProfile(ctx context.Context, addr string) bool {
	_, err := k.MemberProfile.Get(ctx, addr)
	return err == nil
}

// Profile validation

// ValidateDisplayName validates a display name against params constraints
func (k Keeper) ValidateDisplayName(ctx context.Context, name string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	nameLen := uint32(len(name))
	if nameLen < params.DisplayNameMinLength {
		return types.ErrDisplayNameTooShort
	}
	if nameLen > params.DisplayNameMaxLength {
		return types.ErrDisplayNameTooLong
	}
	return nil
}

// ValidateUsername validates a username against params constraints
func (k Keeper) ValidateUsername(ctx context.Context, username string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	usernameLen := uint32(len(username))
	if usernameLen < params.UsernameMinLength {
		return types.ErrUsernameTooShort
	}
	if usernameLen > params.UsernameMaxLength {
		return types.ErrUsernameTooLong
	}

	// Username must be alphanumeric with underscores, lowercase
	validUsername := regexp.MustCompile(`^[a-z0-9_]+$`)
	if !validUsername.MatchString(username) {
		return types.ErrUsernameInvalidChars
	}

	return nil
}

// Guild validation and helpers

// ValidateGuildName validates a guild name
func (k Keeper) ValidateGuildName(ctx context.Context, name string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	nameLen := uint32(len(name))
	if nameLen < 3 {
		return types.ErrGuildNameTooShort
	}
	if nameLen > 50 {
		return types.ErrGuildNameTooLong
	}

	// Check name uniqueness
	iter, err := k.Guild.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	normalizedName := strings.ToLower(name)
	for ; iter.Valid(); iter.Next() {
		guild, err := iter.Value()
		if err != nil {
			continue
		}
		if guild.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED {
			continue
		}
		if strings.ToLower(guild.Name) == normalizedName {
			return types.ErrGuildNameTaken
		}
	}
	_ = params

	return nil
}

// ValidateGuildDescription validates a guild description
func (k Keeper) ValidateGuildDescription(ctx context.Context, description string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	if uint32(len(description)) > params.GuildDescriptionMaxLength {
		return types.ErrGuildDescriptionTooLong
	}
	return nil
}

// IsGuildFounder checks if the address is the founder of the guild
func (k Keeper) IsGuildFounder(ctx context.Context, guildID uint64, addr string) bool {
	guild, err := k.Guild.Get(ctx, guildID)
	if err != nil {
		return false
	}
	return guild.Founder == addr
}

// IsGuildOfficer checks if the address is an officer of the guild
func (k Keeper) IsGuildOfficer(ctx context.Context, guildID uint64, addr string) bool {
	guild, err := k.Guild.Get(ctx, guildID)
	if err != nil {
		return false
	}
	for _, officer := range guild.Officers {
		if officer == addr {
			return true
		}
	}
	return false
}

// IsGuildFounderOrOfficer checks if the address is founder or officer
func (k Keeper) IsGuildFounderOrOfficer(ctx context.Context, guildID uint64, addr string) bool {
	return k.IsGuildFounder(ctx, guildID, addr) || k.IsGuildOfficer(ctx, guildID, addr)
}

// IsGuildMember checks if the address is a member of the guild
func (k Keeper) IsGuildMember(ctx context.Context, guildID uint64, addr string) bool {
	membership, err := k.GuildMembership.Get(ctx, addr)
	if err != nil {
		return false
	}
	return membership.GuildId == guildID && membership.LeftEpoch == 0
}

// GetGuildMemberCount returns the number of members in a guild
func (k Keeper) GetGuildMemberCount(ctx context.Context, guildID uint64) uint64 {
	iter, err := k.GuildMembership.Iterate(ctx, nil)
	if err != nil {
		return 0
	}
	defer iter.Close()

	var count uint64
	for ; iter.Valid(); iter.Next() {
		membership, err := iter.Value()
		if err != nil {
			continue
		}
		if membership.GuildId == guildID && membership.LeftEpoch == 0 {
			count++
		}
	}
	return count
}

// HasPendingGuildInvite checks if there's a pending invite for the member to the guild
func (k Keeper) HasPendingGuildInvite(ctx context.Context, guildID uint64, invitee string) bool {
	key := fmt.Sprintf("%d:%s", guildID, invitee)
	_, err := k.GuildInvite.Get(ctx, key)
	return err == nil
}

// GetPendingInviteCount returns the number of pending invites for a guild
func (k Keeper) GetPendingInviteCount(ctx context.Context, guildID uint64) uint32 {
	guild, err := k.Guild.Get(ctx, guildID)
	if err != nil {
		return 0
	}
	return uint32(len(guild.PendingInvites))
}

// Season state helpers

// GetCurrentSeason returns the current season state as a reptypes.SeasonState.
// This satisfies the reptypes.SeasonKeeper interface used by x/rep.
func (k Keeper) GetCurrentSeason(ctx context.Context) (reptypes.SeasonState, error) {
	season, err := k.Season.Get(ctx)
	if err != nil {
		return reptypes.SeasonState{}, err
	}
	return reptypes.SeasonState{Number: season.Number}, nil
}

// getSeason returns the full internal Season type for use within x/season.
func (k Keeper) getSeason(ctx context.Context) (types.Season, error) {
	return k.Season.Get(ctx)
}

// IsInMaintenanceMode checks if the system is in maintenance mode
func (k Keeper) IsInMaintenanceMode(ctx context.Context) bool {
	state, err := k.SeasonTransitionState.Get(ctx)
	if err != nil {
		return false
	}
	return state.MaintenanceMode
}

// Quest helpers

// HasQuestPrerequisite checks if the member has completed the prerequisite quest
func (k Keeper) HasQuestPrerequisite(ctx context.Context, member string, prerequisiteQuestID string) bool {
	if prerequisiteQuestID == "" {
		return true
	}

	key := fmt.Sprintf("%s:%s", member, prerequisiteQuestID)
	progress, err := k.MemberQuestProgress.Get(ctx, key)
	if err != nil {
		return false
	}
	return progress.Completed
}

// GetMemberActiveQuestCount returns the number of active (in-progress) quests for a member
func (k Keeper) GetMemberActiveQuestCount(ctx context.Context, member string) uint32 {
	iter, err := k.MemberQuestProgress.Iterate(ctx, nil)
	if err != nil {
		return 0
	}
	defer iter.Close()

	var count uint32
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		if !strings.HasPrefix(key, member+":") {
			continue
		}
		progress, err := iter.Value()
		if err != nil {
			continue
		}
		if !progress.Completed {
			count++
		}
	}
	return count
}

// Level calculation

// CalculateLevel calculates the level based on XP and level thresholds
func (k Keeper) CalculateLevel(ctx context.Context, xp uint64) uint64 {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 1
	}

	level := uint64(1)
	for i, threshold := range params.LevelThresholds {
		if xp >= threshold {
			level = uint64(i + 1)
		} else {
			break
		}
	}
	return level
}

// Title helpers

// HasUnlockedTitle checks if the member has unlocked the specified title
func (k Keeper) HasUnlockedTitle(ctx context.Context, member string, titleID string) bool {
	profile, err := k.MemberProfile.Get(ctx, member)
	if err != nil {
		return false
	}
	for _, t := range profile.UnlockedTitles {
		if t == titleID {
			return true
		}
	}
	for _, t := range profile.ArchivedTitles {
		if t == titleID {
			return true
		}
	}
	return false
}

// Cross-module integration helpers

// BurnDREAM burns DREAM tokens from a member's balance via shared dreamutil.Ops.
func (k Keeper) BurnDREAM(ctx context.Context, addr string, amount uint64) error {
	return k.dreamOps.Burn(ctx, addr, amount)
}

// LockDREAM locks (escrows) DREAM tokens via shared dreamutil.Ops.
func (k Keeper) LockDREAM(ctx context.Context, addr string, amount uint64) error {
	return k.dreamOps.Lock(ctx, addr, amount)
}

// UnlockDREAM unlocks (releases) DREAM tokens via shared dreamutil.Ops.
func (k Keeper) UnlockDREAM(ctx context.Context, addr string, amount uint64) error {
	return k.dreamOps.Unlock(ctx, addr, amount)
}

// GetDREAMBalance gets DREAM token balance via x/rep integration
func (k Keeper) GetDREAMBalance(ctx context.Context, addr string) (uint64, error) {
	if k.repKeeper == nil {
		// No rep keeper available - return 0 balance
		return 0, nil
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return 0, err
	}
	balance, err := k.repKeeper.GetBalance(ctx, addrBytes)
	if err != nil {
		return 0, err
	}
	return balance.Uint64(), nil
}

// ReserveName reserves a name via x/name integration
// nameType can be "guild", "username", etc.
func (k Keeper) ReserveName(ctx context.Context, name string, nameType string, owner string) error {
	if k.nameKeeper == nil {
		// No name keeper available - skip name reservation (development mode)
		return nil
	}
	// Check if name is available
	if !k.nameKeeper.IsNameAvailable(ctx, name) {
		return types.ErrNameAlreadyReserved
	}
	// Name reservation would be implemented by the name keeper
	// For now, we rely on local validation since the full x/name integration
	// requires matching the NameRecord type exactly
	return nil
}

// ReleaseName releases a reserved name via x/name integration
func (k Keeper) ReleaseName(ctx context.Context, name string, owner string) error {
	if k.nameKeeper == nil {
		// No name keeper available - skip name release (development mode)
		return nil
	}
	ownerBytes, err := k.addressCodec.StringToBytes(owner)
	if err != nil {
		return err
	}
	return k.nameKeeper.RemoveNameFromOwner(ctx, ownerBytes, name)
}

// CheckNameAvailable checks if a name is available for reservation.
// NOTE: Named differently from NameKeeper.IsNameAvailable to avoid
// accidentally satisfying the NameKeeper interface (which would create
// a depinject self-cycle).
func (k Keeper) CheckNameAvailable(ctx context.Context, name string) bool {
	if k.nameKeeper == nil {
		// No name keeper available - assume available (rely on local validation)
		return true
	}
	return k.nameKeeper.IsNameAvailable(ctx, name)
}
