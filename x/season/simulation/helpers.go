package simulation

import (
	"fmt"
	"math/rand"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// findGuild returns a random guild from state
func findGuild(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Guild, uint64, error) {
	var guilds []struct {
		id    uint64
		guild types.Guild
	}
	err := k.Guild.Walk(ctx, nil, func(id uint64, guild types.Guild) (bool, error) {
		guilds = append(guilds, struct {
			id    uint64
			guild types.Guild
		}{id, guild})
		return false, nil
	})
	if err != nil || len(guilds) == 0 {
		return nil, 0, err
	}
	selected := guilds[r.Intn(len(guilds))]
	return &selected.guild, selected.id, nil
}

// findGuildByFounder returns a guild created by the given founder
func findGuildByFounder(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, founder string) (*types.Guild, uint64, error) {
	var guilds []struct {
		id    uint64
		guild types.Guild
	}
	err := k.Guild.Walk(ctx, nil, func(id uint64, guild types.Guild) (bool, error) {
		if guild.Founder == founder {
			guilds = append(guilds, struct {
				id    uint64
				guild types.Guild
			}{id, guild})
		}
		return false, nil
	})
	if err != nil || len(guilds) == 0 {
		return nil, 0, err
	}
	selected := guilds[r.Intn(len(guilds))]
	return &selected.guild, selected.id, nil
}

// findOpenGuild returns a random non-invite-only guild
func findOpenGuild(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Guild, uint64, error) {
	var guilds []struct {
		id    uint64
		guild types.Guild
	}
	err := k.Guild.Walk(ctx, nil, func(id uint64, guild types.Guild) (bool, error) {
		if !guild.InviteOnly {
			guilds = append(guilds, struct {
				id    uint64
				guild types.Guild
			}{id, guild})
		}
		return false, nil
	})
	if err != nil || len(guilds) == 0 {
		return nil, 0, err
	}
	selected := guilds[r.Intn(len(guilds))]
	return &selected.guild, selected.id, nil
}

// findGuildMembership returns a random guild membership
func findGuildMembership(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.GuildMembership, string, error) {
	var memberships []struct {
		addr       string
		membership types.GuildMembership
	}
	err := k.GuildMembership.Walk(ctx, nil, func(addr string, membership types.GuildMembership) (bool, error) {
		memberships = append(memberships, struct {
			addr       string
			membership types.GuildMembership
		}{addr, membership})
		return false, nil
	})
	if err != nil || len(memberships) == 0 {
		return nil, "", err
	}
	selected := memberships[r.Intn(len(memberships))]
	return &selected.membership, selected.addr, nil
}

// findGuildMemberByGuild returns a member of a specific guild
func findGuildMemberByGuild(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, guildId uint64) (*types.GuildMembership, string, error) {
	var memberships []struct {
		addr       string
		membership types.GuildMembership
	}
	err := k.GuildMembership.Walk(ctx, nil, func(addr string, membership types.GuildMembership) (bool, error) {
		if membership.GuildId == guildId {
			memberships = append(memberships, struct {
				addr       string
				membership types.GuildMembership
			}{addr, membership})
		}
		return false, nil
	})
	if err != nil || len(memberships) == 0 {
		return nil, "", err
	}
	selected := memberships[r.Intn(len(memberships))]
	return &selected.membership, selected.addr, nil
}

// findGuildInvite returns a random pending guild invite
func findGuildInvite(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.GuildInvite, string, error) {
	var invites []struct {
		key    string
		invite types.GuildInvite
	}
	err := k.GuildInvite.Walk(ctx, nil, func(key string, invite types.GuildInvite) (bool, error) {
		invites = append(invites, struct {
			key    string
			invite types.GuildInvite
		}{key, invite})
		return false, nil
	})
	if err != nil || len(invites) == 0 {
		return nil, "", err
	}
	selected := invites[r.Intn(len(invites))]
	return &selected.invite, selected.key, nil
}

// findQuest returns a random quest from state
func findQuest(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Quest, string, error) {
	var quests []struct {
		id    string
		quest types.Quest
	}
	err := k.Quest.Walk(ctx, nil, func(id string, quest types.Quest) (bool, error) {
		quests = append(quests, struct {
			id    string
			quest types.Quest
		}{id, quest})
		return false, nil
	})
	if err != nil || len(quests) == 0 {
		return nil, "", err
	}
	selected := quests[r.Intn(len(quests))]
	return &selected.quest, selected.id, nil
}

// findActiveQuest returns a random active quest
func findActiveQuest(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Quest, string, error) {
	var quests []struct {
		id    string
		quest types.Quest
	}
	err := k.Quest.Walk(ctx, nil, func(id string, quest types.Quest) (bool, error) {
		if quest.Active {
			quests = append(quests, struct {
				id    string
				quest types.Quest
			}{id, quest})
		}
		return false, nil
	})
	if err != nil || len(quests) == 0 {
		return nil, "", err
	}
	selected := quests[r.Intn(len(quests))]
	return &selected.quest, selected.id, nil
}

// findMemberQuestProgress returns a random member quest progress
func findMemberQuestProgress(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.MemberQuestProgress, string, error) {
	var progress []struct {
		key      string
		progress types.MemberQuestProgress
	}
	err := k.MemberQuestProgress.Walk(ctx, nil, func(key string, prog types.MemberQuestProgress) (bool, error) {
		progress = append(progress, struct {
			key      string
			progress types.MemberQuestProgress
		}{key, prog})
		return false, nil
	})
	if err != nil || len(progress) == 0 {
		return nil, "", err
	}
	selected := progress[r.Intn(len(progress))]
	return &selected.progress, selected.key, nil
}

// findMemberProfile returns a random member profile
func findMemberProfile(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.MemberProfile, string, error) {
	var profiles []struct {
		addr    string
		profile types.MemberProfile
	}
	err := k.MemberProfile.Walk(ctx, nil, func(addr string, profile types.MemberProfile) (bool, error) {
		profiles = append(profiles, struct {
			addr    string
			profile types.MemberProfile
		}{addr, profile})
		return false, nil
	})
	if err != nil || len(profiles) == 0 {
		return nil, "", err
	}
	selected := profiles[r.Intn(len(profiles))]
	return &selected.profile, selected.addr, nil
}

// findTitle returns a random title
func findTitle(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Title, string, error) {
	var titles []struct {
		id    string
		title types.Title
	}
	err := k.Title.Walk(ctx, nil, func(id string, title types.Title) (bool, error) {
		titles = append(titles, struct {
			id    string
			title types.Title
		}{id, title})
		return false, nil
	})
	if err != nil || len(titles) == 0 {
		return nil, "", err
	}
	selected := titles[r.Intn(len(titles))]
	return &selected.title, selected.id, nil
}

// findDisplayNameModeration returns a pending display name moderation case
func findDisplayNameModeration(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.DisplayNameModeration, string, error) {
	var mods []struct {
		addr string
		mod  types.DisplayNameModeration
	}
	err := k.DisplayNameModeration.Walk(ctx, nil, func(addr string, mod types.DisplayNameModeration) (bool, error) {
		mods = append(mods, struct {
			addr string
			mod  types.DisplayNameModeration
		}{addr, mod})
		return false, nil
	})
	if err != nil || len(mods) == 0 {
		return nil, "", err
	}
	selected := mods[r.Intn(len(mods))]
	return &selected.mod, selected.addr, nil
}

// getOrCreateGuild returns an existing guild or creates one
func getOrCreateGuild(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, founder string) (uint64, error) {
	// Try to find existing guild by founder
	_, guildID, err := findGuildByFounder(r, ctx, k, founder)
	if err == nil && guildID != 0 {
		return guildID, nil
	}

	// Create new guild
	guildID, err = k.GuildSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	guild := types.Guild{
		Id:          guildID,
		Name:        randomGuildName(r),
		Description: "A simulation generated guild",
		Founder:     founder,
		InviteOnly:  r.Intn(2) == 1,
		CreatedBlock: ctx.BlockHeight(),
		Status:      types.GuildStatus_GUILD_STATUS_ACTIVE,
	}

	if err := k.Guild.Set(ctx, guildID, guild); err != nil {
		return 0, err
	}

	// Create founder membership
	membership := types.GuildMembership{
		Member:      founder,
		GuildId:     guildID,
		JoinedEpoch: 1, // Default epoch
	}

	return guildID, k.GuildMembership.Set(ctx, founder, membership)
}

// getOrCreateQuest returns an existing quest or creates one
func getOrCreateQuest(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (string, error) {
	// Try to find existing active quest
	_, questID, err := findActiveQuest(r, ctx, k)
	if err == nil && questID != "" {
		return questID, nil
	}

	// Create new quest
	questID = randomQuestId(r)
	quest := types.Quest{
		QuestId:        questID,
		Name:           fmt.Sprintf("Quest %s", questID),
		Description:    "A simulation generated quest",
		XpReward:       uint64(50 + r.Intn(200)),
		Repeatable:     r.Intn(2) == 1,
		CooldownEpochs: uint64(r.Intn(5)),
		Active:         true,
	}

	return questID, k.Quest.Set(ctx, questID, quest)
}

// getOrCreateMemberProfile returns an existing member profile or creates one
func getOrCreateMemberProfile(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, member string) error {
	// Check if profile exists
	profile, err := k.MemberProfile.Get(ctx, member)
	if err == nil {
		// Profile exists - make sure GuildId is synced with membership
		membership, mErr := k.GuildMembership.Get(ctx, member)
		if mErr == nil && profile.GuildId != membership.GuildId {
			profile.GuildId = membership.GuildId
			return k.MemberProfile.Set(ctx, member, profile)
		}
		return nil
	}

	// Create new profile
	profile = types.MemberProfile{
		Address:     member,
		DisplayName: randomDisplayName(r),
		Username:    randomUsername(r),
	}

	// Check if member has a guild membership and set GuildId
	membership, mErr := k.GuildMembership.Get(ctx, member)
	if mErr == nil {
		profile.GuildId = membership.GuildId
	}

	return k.MemberProfile.Set(ctx, member, profile)
}

// getOrCreateTitle returns an existing title or creates one
func getOrCreateTitle(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (string, error) {
	// Try to find existing title
	_, titleID, err := findTitle(r, ctx, k)
	if err == nil && titleID != "" {
		return titleID, nil
	}

	// Create new title
	titleID = randomTitleId(r)
	title := types.Title{
		TitleId:     titleID,
		Name:        fmt.Sprintf("Title %s", titleID),
		Description: "A simulation generated title",
	}

	return titleID, k.Title.Set(ctx, titleID, title)
}

// getOrCreateGuildMember creates a membership for a user in a guild
func getOrCreateGuildMember(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, guildId uint64, member string) error {
	// Check if membership exists
	existing, err := k.GuildMembership.Get(ctx, member)
	if err == nil {
		// Membership exists - check if it's for the correct guild
		if existing.GuildId == guildId {
			return nil // Already a member of this guild
		}
		// Member is in a different guild - can't add to this one
		return fmt.Errorf("member %s is already in guild %d", member, existing.GuildId)
	}

	// Create new membership
	membership := types.GuildMembership{
		Member:      member,
		GuildId:     guildId,
		JoinedEpoch: 1, // Default epoch
	}

	return k.GuildMembership.Set(ctx, member, membership)
}

// getOrCreateGuildInvite creates an invite for a user to a guild
func getOrCreateGuildInvite(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, guildId uint64, inviter string, invitee string) error {
	key := fmt.Sprintf("%d:%s", guildId, invitee)
	// Check if invite exists
	_, err := k.GuildInvite.Get(ctx, key)
	if err == nil {
		return nil
	}

	// Create new invite
	invite := types.GuildInvite{
		GuildInvitee: invitee,
		Inviter:      inviter,
		CreatedEpoch: 1,
		ExpiresEpoch: 100,
	}

	if err := k.GuildInvite.Set(ctx, key, invite); err != nil {
		return err
	}

	// Also add to the guild's PendingInvites list
	guild, err := k.Guild.Get(ctx, guildId)
	if err != nil {
		return err
	}
	guild.PendingInvites = append(guild.PendingInvites, invitee)
	return k.Guild.Set(ctx, guildId, guild)
}

// getOrCreateMemberQuestProgress creates quest progress for a member
func getOrCreateMemberQuestProgress(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, member string, questId string) (string, error) {
	key := fmt.Sprintf("%s:%s", member, questId)
	// Check if progress exists
	_, err := k.MemberQuestProgress.Get(ctx, key)
	if err == nil {
		return key, nil
	}

	// Create new progress
	progress := types.MemberQuestProgress{
		MemberQuest: key,
		Completed:   false,
	}

	return key, k.MemberQuestProgress.Set(ctx, key, progress)
}

// randomGuildName generates a random guild name
func randomGuildName(r *rand.Rand) string {
	names := []string{"Warriors", "Explorers", "Guardians", "Pioneers", "Champions", "Legends", "Defenders", "Builders"}
	return fmt.Sprintf("The %s %d", names[r.Intn(len(names))], r.Intn(1000))
}

// randomDisplayName generates a random display name
func randomDisplayName(r *rand.Rand) string {
	prefixes := []string{"Cool", "Epic", "Super", "Mega", "Ultra", "Pro", "Elite", "Master"}
	suffixes := []string{"Player", "Gamer", "User", "Member", "Hero", "Star", "Champion", "Legend"}
	return fmt.Sprintf("%s%s%d", prefixes[r.Intn(len(prefixes))], suffixes[r.Intn(len(suffixes))], r.Intn(1000))
}

// randomUsername generates a random username
func randomUsername(r *rand.Rand) string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	username := make([]byte, 8)
	for i := range username {
		username[i] = chars[r.Intn(len(chars))]
	}
	return string(username)
}

// randomQuestId generates a random quest ID
func randomQuestId(r *rand.Rand) string {
	return fmt.Sprintf("quest_%d", r.Intn(10000))
}

// randomTitleId generates a random title ID
func randomTitleId(r *rand.Rand) string {
	return fmt.Sprintf("title_%d", r.Intn(100))
}

// getAccountForAddress finds a simulation account for the given address
func getAccountForAddress(addr string, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// getAuthority returns the module authority as string
func getAuthority(k keeper.Keeper) string {
	addr, err := k.GetAddressCodec().BytesToString(k.GetAuthority())
	if err != nil {
		return ""
	}
	return addr
}

// isFounder checks if an address is the founder of a guild
func isFounder(ctx sdk.Context, k keeper.Keeper, guildId uint64, addr string) bool {
	guild, err := k.Guild.Get(ctx, guildId)
	if err != nil {
		return false
	}
	return guild.Founder == addr
}

// isOfficer checks if an address is an officer of a guild
func isOfficer(ctx sdk.Context, k keeper.Keeper, guildId uint64, addr string) bool {
	guild, err := k.Guild.Get(ctx, guildId)
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
