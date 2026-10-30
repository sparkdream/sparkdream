package keeper

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
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
	if k.late.commonsKeeper != nil {
		addrBytes, err := k.addressCodec.StringToBytes(addr)
		if err != nil {
			return false
		}
		isMember, err := k.late.commonsKeeper.IsCommitteeMember(ctx, addrBytes, "commons", "operations")
		if err == nil {
			return isMember
		}
	}
	// Fallback: treat governance authority as operations committee
	return k.IsGovAuthority(ctx, addr)
}

// IsAuthorizedForGamification gates quest, achievement and title
// definitions and display-name moderation resolutions. User decision
// (SEA-23): these need a committee decision -- governance, the Commons
// Council policy address or the Commons Operations Committee policy
// address (an executed proposal) -- never an individual committee member.
// Quest XP is further capped by types.MaxQuestXpReward.
func (k Keeper) IsAuthorizedForGamification(ctx context.Context, addr string) bool {
	if k.IsGovAuthority(ctx, addr) {
		return true
	}
	if k.late.commonsKeeper == nil {
		return false
	}
	return k.late.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, addr, "commons", "operations")
}

// IsAuthorizedForTransition gates the season state machine and its clock:
// abort / skip / retry of a transition and the operational params (which
// include epoch_blocks). It accepts governance, the Commons Council policy
// address or the Commons Operations Committee policy address -- an executed
// proposal -- but never an individual committee member (SEA-10).
func (k Keeper) IsAuthorizedForTransition(ctx context.Context, addr string) bool {
	if k.IsGovAuthority(ctx, addr) {
		return true
	}
	if k.late.commonsKeeper == nil {
		return false
	}
	return k.late.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, addr, "commons", "operations")
}

// IsCommonsCouncil gates the season calendar (ExtendSeason,
// SetNextSeasonInfo). It accepts governance, the Commons Council policy
// address, or the policy address of the Commons Operations or Governance
// (hr) Committee -- an executed proposal -- but never an individual
// committee member (SEA-10).
func (k Keeper) IsCommonsCouncil(ctx context.Context, addr string) bool {
	if k.IsGovAuthority(ctx, addr) {
		return true
	}
	if k.late.commonsKeeper == nil {
		return false
	}
	for _, committee := range []string{"operations", "governance"} {
		if k.late.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, addr, "commons", committee) {
			return true
		}
	}
	return false
}

// IsMember checks if the address is a registered member (via x/rep)
func (k Keeper) IsMember(ctx context.Context, addr string) bool {
	// If rep keeper is available, use it
	if k.late.repKeeper != nil {
		accAddr, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return false
		}
		return k.late.repKeeper.IsMember(ctx, accAddr)
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

// displayNameBlockedWords are system/role terms that should not appear in display names.
// This is checked as a substring match (case-insensitive) to catch impersonation attempts
// like "Admin_Bob" or "mod-team". Keep in sync with x/name's BlockedNames for consistency.
var displayNameBlockedWords = []string{
	"admin", "moderator", "system", "support", "official", "team",
	"founder", "validator", "council", "treasury", "governance",
	"sparkdream", "airdrop", "claim", "refund",
}

// ValidateDisplayName validates a display name against params constraints and content filters.
func (k Keeper) ValidateDisplayName(ctx context.Context, name string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	nameLen := commontypes.Saturate[uint32](len(name))
	if nameLen < params.DisplayNameMinLength {
		return types.ErrDisplayNameTooShort
	}
	if nameLen > params.DisplayNameMaxLength {
		return types.ErrDisplayNameTooLong
	}

	if err := validDisplayNameChars(name); err != nil {
		return err
	}

	// Content filtering: check for blocked/impersonation terms (case-insensitive substring)
	lower := strings.ToLower(name)
	for _, blocked := range displayNameBlockedWords {
		if strings.Contains(lower, blocked) {
			return types.ErrDisplayNameBlocked
		}
	}

	return nil
}

// validDisplayNameChars rejects invalid UTF-8, control characters and the
// Unicode format characters (bidi overrides, zero-width joiners and
// spaces), which break client layouts and log lines and help impersonation
// (SEA-34).
func validDisplayNameChars(name string) error {
	if !utf8.ValidString(name) {
		return types.ErrDisplayNameInvalidChars
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return errorsmod.Wrapf(types.ErrDisplayNameInvalidChars, "character U+%04X is not allowed", r)
		}
	}
	return nil
}

// ValidateUsername validates a username against params constraints
func (k Keeper) ValidateUsername(ctx context.Context, username string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	usernameLen := commontypes.Saturate[uint32](len(username))
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

// Guild-name bounds: x/name's default name length (3-30) and character set.
const (
	GuildNameMinLength = 3
	GuildNameMaxLength = 30
)

// validGuildName is x/name's name format (lowercase a-z, 0-9, _ and -, not
// starting or ending with -).
var validGuildName = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?$`)

// ValidateGuildName validates a guild name
func (k Keeper) ValidateGuildName(ctx context.Context, name string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// A guild name is reserved in x/name, so it follows x/name's format:
	// otherwise the reservation would refuse every name CreateGuild accepted.
	if len(name) < GuildNameMinLength {
		return types.ErrGuildNameTooShort
	}
	if len(name) > GuildNameMaxLength {
		return types.ErrGuildNameTooLong
	}
	if !validGuildName.MatchString(name) {
		return types.ErrGuildNameInvalidChars
	}

	// Name uniqueness among live guilds: one prefix read of the ByName
	// index, never a scan of every guild (SEA-28).
	taken := false
	if err := k.Guild.Indexes.ByName.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](strings.ToLower(name)),
		func(_ string, _ uint64) (bool, error) {
			taken = true
			return true, nil
		}); err != nil {
		return err
	}
	if taken {
		return types.ErrGuildNameTaken
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

	if commontypes.Saturate[uint32](len(description)) > params.GuildDescriptionMaxLength {
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
	return k.canActAs(ctx, addr, guild.Founder)
}

// IsGuildOfficer checks if the address is an officer of the guild
func (k Keeper) IsGuildOfficer(ctx context.Context, guildID uint64, addr string) bool {
	guild, err := k.Guild.Get(ctx, guildID)
	if err != nil {
		return false
	}
	for _, officer := range guild.Officers {
		if k.canActAs(ctx, addr, officer) {
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

// GetGuildMemberCount returns the number of active members in a guild from
// the GuildMemberCount counter, which every membership change keeps in step
// (joinGuildMembership / leaveGuildMembership, SEA-24). When the counter is
// absent it is rebuilt from the guild's ByGuild index prefix.
func (k Keeper) GetGuildMemberCount(ctx context.Context, guildID uint64) uint64 {
	count, err := k.GuildMemberCount.Get(ctx, guildID)
	if err == nil {
		return count
	}
	return k.recomputeGuildMemberCount(ctx, guildID)
}

// recomputeGuildMemberCount counts the guild's active members through the
// ByGuild index (bounded by the guild's size) and persists the result.
func (k Keeper) recomputeGuildMemberCount(ctx context.Context, guildID uint64) uint64 {
	count := uint64(len(k.guildMembers(ctx, guildID)))
	if err := k.setGuildMemberCount(ctx, guildID, count); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("recomputeGuildMemberCount: GuildMemberCount.Set failed", "err", err)
	}
	return count
}

// guildMembers lists the addresses holding an active membership in guildID.
func (k Keeper) guildMembers(ctx context.Context, guildID uint64) []string {
	if guildID == 0 {
		return nil
	}
	var members []string
	if err := k.GuildMembership.Indexes.ByGuild.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, string](guildID),
		func(_ uint64, member string) (bool, error) {
			members = append(members, member)
			return false, nil
		}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("guildMembers: GuildMembership.Indexes.ByGuild.Walk failed", "err", err)
	}
	return members
}

// guildsJoinedThisSeason returns how many guilds the membership record has
// created or joined in the current season. The count is stored with its
// season, so a record from an earlier season reads as zero (SEA-27).
func (k Keeper) guildsJoinedThisSeason(ctx context.Context, m types.GuildMembership) uint64 {
	season, err := k.Season.Get(ctx)
	if err != nil || m.GuildsJoinedSeason != season.Number {
		return 0
	}
	return m.GuildsJoinedThisSeason
}

// joinGuildMembership writes an active membership of member in guildID,
// bumps the per-season join count and the guild's active-member counter
// (SEA-24, SEA-27). Callers have checked the member is in no guild.
func (k Keeper) joinGuildMembership(ctx context.Context, member string, guildID uint64, epoch int64) error {
	var season uint64
	if s, err := k.Season.Get(ctx); err == nil {
		season = s.Number
	}
	// Read the counter before the write: with no counter yet it is rebuilt
	// from the ByGuild index, which must not already hold this member.
	count := k.GetGuildMemberCount(ctx, guildID)
	old, err := k.GuildMembership.Get(ctx, member)
	joined := uint64(1)
	if err == nil {
		if activeGuildOf(old) != 0 {
			return types.ErrAlreadyInGuild
		}
		joined = k.guildsJoinedThisSeason(ctx, old) + 1
	}
	if err := k.GuildMembership.Set(ctx, member, types.GuildMembership{
		Member:                 member,
		GuildId:                guildID,
		JoinedEpoch:            epoch,
		LeftEpoch:              0,
		GuildsJoinedThisSeason: joined,
		GuildsJoinedSeason:     season,
	}); err != nil {
		return err
	}
	return k.setGuildMemberCount(ctx, guildID, count+1)
}

// leaveGuildMembership closes member's active membership (if any) and
// decrements its guild's active-member counter. It returns the guild left,
// or 0 when the member held no active membership.
func (k Keeper) leaveGuildMembership(ctx context.Context, member string, epoch int64) (uint64, error) {
	m, err := k.GuildMembership.Get(ctx, member)
	if err != nil {
		return 0, nil
	}
	guildID := activeGuildOf(m)
	if guildID == 0 {
		return 0, nil
	}
	count := k.GetGuildMemberCount(ctx, guildID)
	m.GuildId = 0
	m.LeftEpoch = epoch
	if err := k.GuildMembership.Set(ctx, member, m); err != nil {
		return 0, err
	}
	if count > 0 {
		count--
	}
	return guildID, k.setGuildMemberCount(ctx, guildID, count)
}

// setGuildMemberCount stores a guild's active-member counter, removing it at
// zero so the store matches InitGenesis (which only rebuilds non-zero
// counters); an absent counter reads as a recount of the empty ByGuild range.
func (k Keeper) setGuildMemberCount(ctx context.Context, guildID, count uint64) error {
	if count == 0 {
		return k.GuildMemberCount.Remove(ctx, guildID)
	}
	return k.GuildMemberCount.Set(ctx, guildID, count)
}

// checkGuildHopCooldown refuses joining or creating a guild within
// guild_hop_cooldown_epochs of leaving one. A membership record holding no
// active guild is a past membership, including one left in epoch 0.
func checkGuildHopCooldown(m types.GuildMembership, found bool, currentEpoch int64, cooldownEpochs uint64) error {
	if !found || activeGuildOf(m) != 0 {
		return nil
	}
	since := commontypes.Saturate[uint64](currentEpoch - m.LeftEpoch)
	if since < cooldownEpochs {
		return errorsmod.Wrapf(types.ErrGuildHopCooldown, "must wait %d more epochs", cooldownEpochs-since)
	}
	return nil
}

// guildInviteLive reports whether invitee holds an unexpired invite to the
// guild. An expired invite stays stored until pruning but grants nothing.
func (k Keeper) guildInviteLive(ctx context.Context, guildID uint64, invitee string) bool {
	invite, err := k.GuildInvite.Get(ctx, fmt.Sprintf("%d:%s", guildID, invitee))
	if err != nil {
		return false
	}
	return invite.ExpiresEpoch == 0 || k.GetCurrentEpoch(ctx) <= invite.ExpiresEpoch
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
	return commontypes.Saturate[uint32](len(guild.PendingInvites))
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

// GetMemberActiveQuestCount returns the number of active (in-progress)
// quests for a member. It counts the member's keys in the Open index, so it
// reads only that member's open quests, never the whole progress table
// (SEA-28).
func (k Keeper) GetMemberActiveQuestCount(ctx context.Context, member string) uint32 {
	var count uint32
	if err := k.MemberQuestProgress.Indexes.Open.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](member+questOpenSuffix),
		func(_ string, _ string) (bool, error) {
			count++
			return false, nil
		}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("GetMemberActiveQuestCount: MemberQuestProgress.Indexes.Open.Walk failed", "err", err)
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
			level = commontypes.Saturate[uint64](i + 1)
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
	return k.late.dreamOps.Burn(ctx, addr, amount)
}

// LockDREAM locks (escrows) DREAM tokens via shared dreamutil.Ops.
func (k Keeper) LockDREAM(ctx context.Context, addr string, amount uint64) error {
	return k.late.dreamOps.Lock(ctx, addr, amount)
}

// UnlockDREAM unlocks (releases) DREAM tokens via shared dreamutil.Ops.
func (k Keeper) UnlockDREAM(ctx context.Context, addr string, amount uint64) error {
	return k.late.dreamOps.Unlock(ctx, addr, amount)
}

// GetDREAMBalance gets DREAM token balance via x/rep integration
func (k Keeper) GetDREAMBalance(ctx context.Context, addr string) (uint64, error) {
	if k.late.repKeeper == nil {
		// No rep keeper available - return 0 balance
		return 0, nil
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return 0, err
	}
	balance, err := k.late.repKeeper.GetBalance(ctx, addrBytes)
	if err != nil {
		return 0, err
	}
	return balance.Uint64(), nil
}

// ReserveName atomically claims a name via x/name integration.
// nameType can be "guild", "username", etc. and is stored as the record's Data field.
func (k Keeper) ReserveName(ctx context.Context, name string, nameType string, owner string) error {
	if k.late.nameKeeper == nil {
		// No name keeper available - skip name reservation (development mode)
		return nil
	}
	// Atomically check availability and register in one step to prevent
	// TOCTOU races within the same block.
	if err := k.late.nameKeeper.ClaimName(ctx, name, owner, nameType); err != nil {
		return errorsmod.Wrap(types.ErrNameAlreadyReserved, err.Error())
	}
	return nil
}

// ReleaseName releases a reserved name via x/name integration
func (k Keeper) ReleaseName(ctx context.Context, name string, owner string) error {
	if k.late.nameKeeper == nil {
		// No name keeper available - skip name release (development mode)
		return nil
	}
	return k.late.nameKeeper.ReleaseName(ctx, name, owner)
}

// CheckNameAvailable checks if a name is available for reservation.
// NOTE: Named differently from NameKeeper.IsNameAvailable to avoid
// accidentally satisfying the NameKeeper interface (which would create
// a depinject self-cycle).
func (k Keeper) CheckNameAvailable(ctx context.Context, name string) bool {
	if k.late.nameKeeper == nil {
		// No name keeper available - assume available (rely on local validation)
		return true
	}
	return k.late.nameKeeper.IsNameAvailable(ctx, name)
}
